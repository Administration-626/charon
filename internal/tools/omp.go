package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Oh My Pi (omp) is a Pi fork that keeps custom providers in
// ~/.omp/agent/models.yml under providers.<id>, and picks the model a session
// starts on from the "provider/id" value of modelRoles.default in config.yml.
// charon owns the providers.charon block and edits both YAML files through
// their node trees so user keys, order, and comments survive. OAuth logins and
// the auth store (~/.omp/agent/agent.db) are the tool's own business and are
// never read or written.

// ompAPI is the wire dialect every charon-registered omp model speaks; omp
// dispatches on this field, not on the provider id.
const ompAPI = "openai-completions"

// ompModel is one entry of a models.yml provider's models list.
type ompModel struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	Reasoning     bool     `yaml:"reasoning"`
	Input         []string `yaml:"input"`
	ContextWindow int      `yaml:"contextWindow"`
	MaxTokens     int      `yaml:"maxTokens"`
}

// ompProvider is the providers.charon block charon owns in models.yml.
type ompProvider struct {
	BaseURL string     `yaml:"baseUrl"`
	APIKey  string     `yaml:"apiKey,omitempty"`
	API     string     `yaml:"api"`
	Models  []ompModel `yaml:"models"`
}

// ompFile returns the file omp reads for a logical name: the canonical .yml
// path, or an existing .yaml sibling, which omp itself keeps updating in place.
func ompFile(dir, base string) string {
	canonical := filepath.Join(dir, base+".yml")
	if _, err := os.Stat(canonical); err == nil {
		return canonical
	}
	legacy := filepath.Join(dir, base+".yaml")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return canonical
}

// ompBuildModels turns model ids into omp model entries with the same shape
// charon's pi extension registers, so both tools size a custom model alike.
func ompBuildModels(ids []string) []ompModel {
	models := make([]ompModel, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		models = append(models, ompModel{
			ID:            id,
			Name:          id,
			Input:         []string{"text", "image"},
			ContextWindow: 128000,
			MaxTokens:     8192,
		})
	}
	return models
}

// ompReadProvider decodes providers.<name> from a models.yml document.
func ompReadProvider(doc *yaml.Node, name string) (ompProvider, bool) {
	root, err := yamlDocMap(doc)
	if err != nil {
		return ompProvider{}, false
	}
	node := yamlMapEntry(yamlMapEntry(root, "providers"), name)
	if node == nil || node.Kind != yaml.MappingNode {
		return ompProvider{}, false
	}
	var p ompProvider
	if node.Decode(&p) != nil {
		return ompProvider{}, false
	}
	return p, true
}

// ompDecodeProviders decodes every providers entry, charon's included, into
// plain Go values for the shared ensureOnlyCharonChanged guard.
func ompDecodeProviders(providers *yaml.Node) map[string]any {
	all := map[string]any{}
	if providers == nil || providers.Kind != yaml.MappingNode {
		return all
	}
	for i := 0; i+1 < len(providers.Content); i += 2 {
		var v any
		if providers.Content[i+1].Decode(&v) == nil {
			all[providers.Content[i].Value] = v
		}
	}
	return all
}

// ompSetDefaultModel points modelRoles.default at charon's provider for slug,
// leaving every other role and setting untouched.
func ompSetDefaultModel(dir, slug string) error {
	path := ompFile(dir, "config")
	doc, err := loadYAMLDoc(path)
	if err != nil {
		return err
	}
	root, err := yamlDocMap(doc)
	if err != nil {
		return err
	}
	roles, err := yamlMapChild(root, "modelRoles")
	if err != nil {
		return err
	}
	yamlSetValue(roles, "default", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: managedProvider + "/" + slug})
	if err := writeYAMLDoc(path, doc, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// newOmp describes Oh My Pi (~/.omp/agent). Its model picker (/model, alias
// /models) lists whatever models the active provider registers in models.yml.
func newOmp() *Tool {
	dir := filepath.Join(home(), ".omp", "agent")
	modelsPath := ompFile(dir, "models")
	configPath := ompFile(dir, "config")
	authPath := filepath.Join(dir, "agent.db")

	return &Tool{
		Name:            "omp",
		Title:           "Oh My Pi",
		Provider:        "openai",
		ModelMenu:       "/model",
		DefaultEndpoint: "https://api.openai.com/v1",
		ApplyAuth: func(a AuthSpec) error {
			doc, err := loadYAMLDoc(modelsPath)
			if err != nil {
				return err
			}
			root, err := yamlDocMap(doc)
			if err != nil {
				return err
			}
			providers, err := yamlMapChild(root, "providers")
			if err != nil {
				return err
			}
			before := snapshotProviders(ompDecodeProviders(providers))

			// A call without its own list keeps what is registered (rename, key
			// rotation); Catalog.Project always passes the binding's full list, so a
			// switch replaces the picker instead of retaining another endpoint's models.
			ids := a.AllModels
			if len(ids) == 0 {
				if prev, ok := ompReadProvider(doc, managedProvider); ok {
					for _, m := range prev.Models {
						ids = append(ids, m.ID)
					}
				}
			}
			modelSlug := strings.TrimSpace(a.Model)
			if len(ids) == 0 && modelSlug != "" {
				ids = []string{modelSlug}
			}

			var node yaml.Node
			if err := node.Encode(ompProvider{
				BaseURL: a.Endpoint,
				APIKey:  a.Key,
				API:     ompAPI,
				Models:  ompBuildModels(ids),
			}); err != nil {
				return fmt.Errorf("render omp provider: %w", err)
			}
			yamlSetValue(providers, managedProvider, &node)

			if err := ensureOnlyCharonChanged(before, ompDecodeProviders(providers)); err != nil {
				return err
			}
			if err := writeYAMLDoc(modelsPath, doc, 0o600); err != nil {
				return fmt.Errorf("write %s: %w", modelsPath, err)
			}
			if modelSlug == "" {
				return nil
			}
			return ompSetDefaultModel(dir, modelSlug)
		},
		Detected: func() bool {
			return detected("omp", modelsPath, configPath, authPath)
		},
		Describe: func() (Info, error) {
			var info Info

			// modelRoles.default pins the model a new session starts on. The role is
			// "provider/id", so strip charon's own prefix for display; a role pointing
			// at another provider is reported as-is.
			cfg, err := loadYAMLDoc(configPath)
			if err != nil {
				return info, err
			}
			role := ""
			if root, err := yamlDocMap(cfg); err == nil {
				if node := yamlMapEntry(yamlMapEntry(root, "modelRoles"), "default"); node != nil {
					role = strings.TrimSpace(node.Value)
				}
				if level := yamlMapEntry(root, "defaultThinkingLevel"); level != nil {
					info.Effort = level.Value
				}
			}
			if slug, ok := strings.CutPrefix(role, managedProvider+"/"); ok {
				info.Model = slug
			} else if role != "" {
				info.Model = role
			}

			// Report the registered endpoint and key only while charon's provider is
			// the pinned one, or when no role is set at all — the same rule the pi
			// adapter applies to its defaultProvider.
			if role == "" || strings.HasPrefix(role, managedProvider+"/") {
				doc, err := loadYAMLDoc(modelsPath)
				if err != nil {
					return info, err
				}
				if p, ok := ompReadProvider(doc, managedProvider); ok {
					info.Endpoint = p.BaseURL
					if strings.TrimSpace(p.APIKey) != "" {
						info.Secret, info.AuthMode = p.APIKey, "api"
					}
				}
			}
			return info.withDefaults("(provider default)"), nil
		},
	}
}
