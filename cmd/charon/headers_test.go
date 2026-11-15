package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func assertPiHeaders(t *testing.T, home, endpoint string, want map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Providers map[string]struct {
			BaseURL string            `json:"baseUrl"`
			Headers map[string]string `json:"headers"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Providers {
		if p.BaseURL == endpoint {
			if !reflect.DeepEqual(p.Headers, want) {
				t.Fatalf("headers for %s = %#v, want %#v", endpoint, p.Headers, want)
			}
			return
		}
	}
	t.Fatalf("provider for %s is missing", endpoint)
}

func TestRunPiHeadersLifecycle(t *testing.T) {
	home := sandbox(t)
	alpha, beta := "https://alpha.example/v1", "https://beta.example/v1"
	alphaHeaders := map[string]string{"Originator": "alpha-client", "User-Agent": "custom/1.0"}
	betaHeaders := map[string]string{"Originator": "beta-client"}
	for _, args := range [][]string{
		{"add", "pi", "--name", "alpha", "--endpoint", alpha, "--key", "sk-alpha", "--model", "custom", "--headers", `{"Originator":"alpha-client","User-Agent":"custom/1.0"}`},
		{"add", "pi", "--name", "beta", "--endpoint", beta, "--key", "sk-beta", "--model", "custom", "--headers", `{"Originator":"beta-client"}`},
		{"switch", "pi", "alpha"},
		{"edit", "pi", "alpha", "--key", "sk-rotated"},
		{"rename", "pi", "alpha", "renamed"},
	} {
		if err := run(args); err != nil {
			t.Fatal(err)
		}
	}
	assertPiHeaders(t, home, alpha, alphaHeaders)
	assertPiHeaders(t, home, beta, betaHeaders)
	modelsPath := filepath.Join(home, ".pi", "agent", "models.json")
	before, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		tool string
		want map[string]string
	}{
		{[]string{"cp", "pi", "renamed", "copy"}, "pi", alphaHeaders},
		{[]string{"cp", "pi", "renamed", "claude", "copy"}, "claude", nil},
	} {
		if err := run(tc.args); err != nil {
			t.Fatal(err)
		}
		b, found, err := openCatalog(t).BindingByName(tc.tool, "copy")
		if err != nil || !found || !reflect.DeepEqual(b.Headers, tc.want) {
			t.Fatalf("copy to %s headers = %#v, err=%v", tc.tool, b.Headers, err)
		}
	}
	after, err := os.ReadFile(modelsPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("copy changed live Pi config")
	}

	for _, tc := range []struct {
		value string
		want  map[string]string
	}{
		{`{"X-Client":"replacement"}`, map[string]string{"X-Client": "replacement"}},
		{`{}`, map[string]string{}},
	} {
		for _, args := range [][]string{
			{"edit", "pi", "renamed", "--headers", tc.value},
			{"switch", "pi", "beta"},
			{"switch", "pi", "renamed"},
			{"reapply", "pi", "renamed"},
		} {
			if err := run(args); err != nil {
				t.Fatal(err)
			}
			assertPiHeaders(t, home, alpha, tc.want)
			assertPiHeaders(t, home, beta, betaHeaders)
		}
		b, found, err := openCatalog(t).BindingByName("pi", "renamed")
		if err != nil || !found || !reflect.DeepEqual(b.Headers, tc.want) {
			t.Fatalf("header override did not persist: %#v, %v", b.Headers, err)
		}
	}
	if err := run([]string{"cp", "pi", "renamed", "empty-copy"}); err != nil {
		t.Fatal(err)
	}
	cloned, found, err := openCatalog(t).BindingByName("pi", "empty-copy")
	if err != nil || !found || cloned.Headers == nil || len(cloned.Headers) != 0 {
		t.Fatalf("copy lost explicit empty override: %#v, %v", cloned.Headers, err)
	}
}

func TestRunRejectsInvalidHeadersWithoutWritingOrExposingValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tool  string
		value string
	}{
		{"empty", "pi", ""},
		{"broken", "pi", `{"X-Key":"private-value"`},
		{"array", "pi", `[]`},
		{"null object", "pi", `null`},
		{"null value", "pi", `{"X-Key":null}`},
		{"number", "pi", `{"X-Key":123}`},
		{"invalid name", "pi", `{"Bad Name":"private-value"}`},
		{"newline", "pi", `{"X-Key":"private-value\n"}`},
		{"case collision", "pi", `{"X-Key":"private-value","x-key":"other"}`},
		{"non-pi", "claude", `{"X-Key":"private-value"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sandbox(t)
			c := openCatalog(t)
			err := run([]string{"add", tc.tool, "--name", "bad", "--key", "sk-test", "--model", "custom", "--headers", tc.value})
			if err == nil || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("invalid headers accepted or exposed: %v", err)
			}
			credentialPath := filepath.Join(c.Root, "credentials.json")
			if _, err := os.Stat(credentialPath); !os.IsNotExist(err) {
				t.Fatalf("invalid add created credentials.json: %v", err)
			}
			if err := run([]string{"add", tc.tool, "--name", "work", "--key", "sk-test", "--model", "custom"}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(c.Root, "bindings.json")
			before := map[string][]byte{}
			for _, file := range []string{path, credentialPath} {
				before[file], err = os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
			}
			err = run([]string{"edit", tc.tool, "work", "--key", "sk-orphan", "--headers", tc.value})
			if err == nil || strings.Contains(err.Error(), "private-value") {
				t.Fatalf("invalid edit accepted or exposed: %v", err)
			}
			for file, want := range before {
				after, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(want, after) {
					t.Fatalf("invalid edit changed %s: %v", filepath.Base(file), err)
				}
			}
		})
	}
}
