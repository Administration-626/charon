package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// models.dev is a free, keyless catalog of model metadata (windows, output
// limits, names) maintained by the community. It fills a context window for
// slugs the builtin table does not know, and — unlike the builtin table —
// refreshes itself: the JSON is cached under the user's cache dir and refetched
// about once a week, so a model released this morning is covered without a
// charon release.

// modelsDevURL is where the catalog comes from; a var for tests.
var modelsDevURL = "https://models.dev/api.json"

// modelsDevTTL is how long a cached catalog is trusted before a lookup
// refetches it.
const modelsDevTTL = 7 * 24 * time.Hour

// modelsDevOnce guards the single load of the catalog per process.
var modelsDevOnce struct {
	sync.Mutex
	loaded bool
	index  map[string][]modelsDevHit // model id → providers listing it
}

type modelsDevHit struct {
	provider string
	context  int
	output   int
}

// DevWindow is the context window models.dev records for slug, from the
// cached catalog (refetching it first when stale). ok is false when the slug is
// unknown, the catalog cannot be fetched, or the entry carries no limit — the
// builtin table remains the last resort.
func DevWindow(slug string) (int, bool) {
	hits, ok := modelsDevLookup(slug)
	if !ok {
		return 0, false
	}
	return hits[0].context, hits[0].context > 0
}

// DevOutput is the max output tokens models.dev records for slug.
func DevOutput(slug string) (int, bool) {
	hits, ok := modelsDevLookup(slug)
	if !ok {
		return 0, false
	}
	return hits[0].output, hits[0].output > 0
}

// modelsDevLookup resolves slug to the providers that list it, best first:
// the provider whose models.dev id matches the slug's vendor prefix (say
// deepseek for deepseek/deepseek-chat) ahead of the rest in a stable
// (alphabetical) order, so a disagreement between relays resolves the same
// way every time.
func modelsDevLookup(slug string) ([]modelsDevHit, bool) {
	slug = strings.TrimSpace(slug)
	if slug == "" || os.Getenv("CHARON_MODELS_DEV") == "off" {
		return nil, false
	}
	index := modelsDevIndex()
	hits := index[slug]
	if len(hits) == 0 {
		return nil, false
	}
	vendor, _, slashed := strings.Cut(slug, "/")
	if !slashed {
		return hits, true
	}
	sort.SliceStable(hits, func(i, j int) bool {
		vi, vj := hits[i].provider == vendor, hits[j].provider == vendor
		if vi != vj {
			return vi
		}
		return hits[i].provider < hits[j].provider
	})
	return hits, true
}

// modelsDevIndex loads the catalog once per process: the cache file when it is
// fresh, else a refetch (falling back to a stale cache offline). A load that
// fails everywhere leaves the index empty — lookups answer false, callers fall
// back to the builtin table, nothing blocks.
func modelsDevIndex() map[string][]modelsDevHit {
	d := &modelsDevOnce
	d.Lock()
	defer d.Unlock()
	if d.loaded {
		return d.index
	}
	d.loaded = true
	d.index = map[string][]modelsDevHit{}

	path := modelsDevCachePath()
	if fresh(path) {
		if data, err := os.ReadFile(path); err == nil {
			if idx, err := parseModelsDev(data); err == nil {
				d.index = idx
				return d.index
			}
		}
	}
	if data, idx, err := fetchModelsDev(); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		if werr := os.WriteFile(path, data, 0o644); werr != nil {
			_ = werr // a cache that cannot be written is refetched next time
		}
		d.index = idx
		return d.index
	}
	// offline: a stale cache still beats nothing
	if data, err := os.ReadFile(path); err == nil {
		if idx, err := parseModelsDev(data); err == nil {
			d.index = idx
		}
	}
	return d.index
}

// modelsDevCachePath is where the catalog is cached. CHARON_MODELS_DEV
// overrides it: "off" disables the source entirely (tests, offline machines),
// any other value is the cache file itself (a mirror, an air-gapped copy —
// trusted as long as refetching keeps failing). Otherwise $XDG_CACHE_HOME is
// respected.
func modelsDevCachePath() string {
	if v := os.Getenv("CHARON_MODELS_DEV"); v != "" {
		return v
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			base = os.TempDir()
		} else {
			base = filepath.Join(home, ".cache")
		}
	}
	return filepath.Join(base, "charon", "models-dev.json")
}

func fresh(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && time.Since(fi.ModTime()) < modelsDevTTL
}

// parseModelsDev turns the catalog JSON into the lookup index. A provider
// entry that does not parse is skipped whole rather than failing the catalog.
func parseModelsDev(data []byte) (map[string][]modelsDevHit, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	index := map[string][]modelsDevHit{}
	providers := 0
	for pid, provRaw := range raw {
		var prov struct {
			Models map[string]struct {
				Limit *struct {
					Context int `json:"context"`
					Output  int `json:"output"`
				} `json:"limit"`
			} `json:"models"`
		}
		if err := json.Unmarshal(provRaw, &prov); err != nil || len(prov.Models) == 0 {
			continue
		}
		providers++
		for mid, m := range prov.Models {
			if mid == "" || m.Limit == nil || m.Limit.Context <= 0 {
				continue
			}
			index[mid] = append(index[mid], modelsDevHit{provider: pid, context: m.Limit.Context, output: m.Limit.Output})
		}
	}
	if providers == 0 {
		return nil, fmt.Errorf("no providers in catalog")
	}
	return index, nil
}

func fetchModelsDev() ([]byte, map[string][]modelsDevHit, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(modelsDevURL)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("models.dev returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, nil, err
	}
	idx, err := parseModelsDev(data)
	if err != nil {
		return nil, nil, err
	}
	return data, idx, nil
}

// ResetModelsDev forgets the loaded catalog so the next lookup reloads it —
// for tests that change CHARON_MODELS_DEV within one process.
func ResetModelsDev() {
	d := &modelsDevOnce
	d.Lock()
	defer d.Unlock()
	d.loaded = false
	d.index = nil
}
