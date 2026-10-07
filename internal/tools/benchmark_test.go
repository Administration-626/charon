package tools

import (
	"fmt"
	"testing"
)

var benchmarkInfo Info

// BenchmarkDescribe includes warm filesystem reads and JSON/TOML decoding.
// Fixture generation is excluded; no real credentials or provider APIs are used.
func BenchmarkDescribe(b *testing.B) {
	for _, name := range []string{"pi", "codex"} {
		for _, count := range []int{1, 100, 1000} {
			b.Run(fmt.Sprintf("%s/models_%d", name, count), func(b *testing.B) {
				b.Setenv("HOME", b.TempDir())
				b.Setenv("XDG_CONFIG_HOME", b.TempDir())
				b.Setenv("PATH", b.TempDir())
				specs := make([]ModelSpec, count)
				for i := range specs {
					specs[i] = ModelSpec{Slug: fmt.Sprintf("bench-model-%04d", i), ContextWindow: 500000}
				}
				tool := Find(name)
				if err := tool.ApplyAuth(AuthSpec{
					Endpoint: "https://benchmark.example/v1", Key: "sk-benchmark-only",
					Model: specs[count-1].Slug, Models: specs,
				}); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					info, err := tool.Describe()
					if err != nil {
						b.Fatal(err)
					}
					benchmarkInfo = info
				}
			})
		}
	}
}
