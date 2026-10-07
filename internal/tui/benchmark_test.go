package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"charon/internal/catalog"
	"charon/internal/tools"
)

// benchmarkMenu seeds private, read-only catalog fixtures directly so benchmark
// setup does not repeatedly rewrite large tables through the mutation API.
func benchmarkMenu(b *testing.B, bindings, modelCount int) model {
	b.Helper()
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	b.Setenv("PATH", b.TempDir()) // also prevents any real Keychain lookup
	root := filepath.Join(home, ".config", "charon")
	if err := os.MkdirAll(root, 0o700); err != nil {
		b.Fatal(err)
	}
	rows := make([]catalog.Model, modelCount)
	ids := make([]string, modelCount)
	specs := make([]tools.ModelSpec, modelCount)
	for i := range rows {
		ids[i] = fmt.Sprintf("m%d", i)
		slug := fmt.Sprintf("bench-model-%04d", i)
		rows[i] = catalog.Model{
			ID: ids[i], ProviderID: "p1", Slug: slug,
			ContextWindow: 500000, ContextWindowSource: catalog.WindowManual,
		}
		specs[i] = tools.ModelSpec{Slug: slug, ContextWindow: 500000}
	}
	saved := make([]catalog.Binding, bindings)
	for i := range saved {
		saved[i] = catalog.Binding{
			ID: fmt.Sprintf("b%d", i), Name: fmt.Sprintf("binding-%03d", i),
			Tool: "pi", CredentialID: "k1", ModelID: ids[0], Models: ids,
		}
	}
	for name, value := range map[string]any{
		"providers.json":   []catalog.Provider{{ID: "p1", BaseURL: "https://benchmark.example/v1"}},
		"credentials.json": []catalog.Credential{{ID: "k1", ProviderID: "p1", Key: "sk-benchmark-only"}},
		"models.json":      rows,
		"bindings.json":    saved,
		"active.json":      map[string]string{"pi": saved[0].ID},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			b.Fatal(err)
		}
	}
	tool := tools.Find("pi")
	if err := tool.ApplyAuth(tools.AuthSpec{
		Endpoint: "https://benchmark.example/v1", Key: "sk-benchmark-only",
		Model: specs[0].Slug, Models: specs,
	}); err != nil {
		b.Fatal(err)
	}
	m := newModel(&catalog.Catalog{Root: root}, "benchmark")
	m.tool, m.view = tool, viewProfiles
	m.width, m.height = 100, 30
	m.resize()
	return m
}

func BenchmarkLoadProfiles(b *testing.B) {
	for _, size := range []int{10, 100} {
		b.Run(fmt.Sprintf("bindings_%d_models_%d", size, size), func(b *testing.B) {
			m := benchmarkMenu(b, size, size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.loadProfiles("")
			}
		})
	}
}

var benchmarkView string

func BenchmarkBindingView(b *testing.B) {
	m := benchmarkMenu(b, 10, 10)
	m.loadProfiles("")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkView = m.View()
	}
}
