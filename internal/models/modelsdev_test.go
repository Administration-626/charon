package models

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// staleTime is older than the catalog TTL, so a file stamped with it refetches.
var staleTime = time.Now().Add(-2 * modelsDevTTL)

// useModelsDev points the catalog at a fixture file for one test.
func useModelsDev(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models-dev.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHARON_MODELS_DEV", path)
	ResetModelsDev()
	t.Cleanup(ResetModelsDev)
}

const fixture = `{
  "deepseek": {
    "id": "deepseek",
    "models": {
      "deepseek-chat": { "limit": { "context": 64000, "output": 8192 } },
      "deepseek-v4-pro": { "limit": { "context": 1000000, "output": 393216 } }
    }
  },
  "relay-a": {
    "models": {
      "deepseek/deepseek-chat": { "limit": { "context": 99000, "output": 4096 } },
      "gpt-x": { "limit": { "context": 128000, "output": 8192 } }
    }
  },
  "relay-b": {
    "models": {
      "gpt-x": { "limit": { "context": 128000, "output": 8192 } },
      "no-limit": { "name": "entry without a limit" }
    }
  },
  "not-a-provider": { "error": "skip me" }
}`

func TestDevWindowFromFixture(t *testing.T) {
	useModelsDev(t, fixture)
	for slug, want := range map[string]int{
		"deepseek-chat":            64000, // bare id
		"deepseek/deepseek-chat":   99000, // exact full slug (relay listing)
		"deepseek/deepseek-v4-pro": 0,     // no relay lists the full slug
		"gpt-x":                    128000,
		"no-limit":                 0, // entry without a limit is skipped
		"something-else":           0,
	} {
		w, ok := DevWindow(slug)
		if want == 0 {
			if ok {
				t.Errorf("DevWindow(%q) = %d, want not ok", slug, w)
			}
			continue
		}
		if !ok || w != want {
			t.Errorf("DevWindow(%q) = %d, %v; want %d", slug, w, ok, want)
		}
	}
	if o, ok := DevOutput("deepseek-v4-pro"); !ok || o != 393216 {
		t.Errorf("DevOutput = %d, %v; want 393216", o, ok)
	}
}

func TestModelsDevVendorPrefixPreferred(t *testing.T) {
	// deepseek lists "deepseek/deepseek-chat" itself while two relays do too:
	// the vendor-prefixed provider wins over the alphabetical relay order.
	doc := `{
	  "a-relay":  { "models": { "deepseek/deepseek-chat": { "limit": { "context": 111000, "output": 1 } } } },
	  "deepseek": { "models": { "deepseek/deepseek-chat": { "limit": { "context": 64000, "output": 2 } } } },
	  "z-relay":  { "models": { "deepseek/deepseek-chat": { "limit": { "context": 222000, "output": 3 } } } }
	}`
	useModelsDev(t, doc)
	w, ok := DevWindow("deepseek/deepseek-chat")
	if !ok || w != 64000 {
		t.Errorf("vendor-prefixed provider must win: %d, %v", w, ok)
	}
}

func TestModelsDevOff(t *testing.T) {
	useModelsDev(t, fixture)
	t.Setenv("CHARON_MODELS_DEV", "off")
	if w, ok := DevWindow("gpt-x"); ok {
		t.Errorf("off must disable the source, got %d", w)
	}
}

func TestModelsDevFetchCachesAndReuses(t *testing.T) {
	fetches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixture))
	}))
	defer srv.Close()
	oldURL := modelsDevURL
	modelsDevURL = srv.URL
	defer func() { modelsDevURL = oldURL }()

	cache := filepath.Join(t.TempDir(), "models-dev.json")
	t.Setenv("CHARON_MODELS_DEV", cache)
	ResetModelsDev()
	t.Cleanup(ResetModelsDev)

	if w, ok := DevWindow("gpt-x"); !ok || w != 128000 {
		t.Fatalf("first lookup = %d, %v", w, ok)
	}
	if fetches != 1 {
		t.Fatalf("fetches = %d, want 1", fetches)
	}
	if b, err := os.ReadFile(cache); err != nil || len(b) == 0 {
		t.Fatalf("cache not written: %v", err)
	}
	// A fresh cache answers without refetching.
	ResetModelsDev()
	if _, ok := DevWindow("gpt-x"); !ok || fetches != 1 {
		t.Errorf("fresh cache must not refetch: fetches=%d ok=%v", fetches, ok)
	}
	// A stale cache is refreshed.
	if err := os.Chtimes(cache, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}
	ResetModelsDev()
	if _, ok := DevWindow("gpt-x"); !ok || fetches != 2 {
		t.Errorf("stale cache must refetch: fetches=%d ok=%v", fetches, ok)
	}
}

func TestModelsDevFallsBackToStaleCacheOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	oldURL := modelsDevURL
	modelsDevURL = srv.URL
	defer func() { modelsDevURL = oldURL }()

	cache := filepath.Join(t.TempDir(), "models-dev.json")
	os.WriteFile(cache, []byte(fixture), 0o644)
	os.Chtimes(cache, staleTime, staleTime)
	t.Setenv("CHARON_MODELS_DEV", cache)
	ResetModelsDev()
	t.Cleanup(ResetModelsDev)

	if w, ok := DevWindow("gpt-x"); !ok || w != 128000 {
		t.Errorf("stale cache must serve offline: %d, %v", w, ok)
	}
}

func TestModelsDevBadCatalogIgnored(t *testing.T) {
	useModelsDev(t, "not json at all")
	if w, ok := DevWindow("gpt-x"); ok {
		t.Errorf("bad catalog must answer false, got %d", w)
	}
}
