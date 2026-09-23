package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charon/internal/artifact"
	"charon/internal/tools"
)

const legacyMigrationMarker = ".legacy-profiles-migrated"

type legacyProfileConfig struct {
	Active map[string]string `json:"active"`
}

type legacyProfileManifest struct {
	Spec *struct {
		Endpoint string   `json:"endpoint"`
		Key      string   `json:"key"`
		Model    string   `json:"model"`
		Models   []string `json:"models"`
	} `json:"spec"`
}

// migrateLegacyProfiles imports editable endpoint/key/model profiles once. Snapshot
// only profiles (including OAuth logins) are left untouched in the legacy directory.
func (c *Catalog) migrateLegacyProfiles() error {
	marker := c.table(legacyMigrationMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	legacyRoot := c.table("profiles")
	if _, err := os.Stat(legacyRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	var legacyCfg legacyProfileConfig
	if data, err := os.ReadFile(c.table("config.json")); err == nil {
		_ = json.Unmarshal(data, &legacyCfg)
	}

	imported := make(map[string]Binding)
	for _, t := range tools.All() {
		toolDir := filepath.Join(legacyRoot, t.Name)
		entries, err := os.ReadDir(toolDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading legacy %s profiles: %w", t.Name, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if validateName(name) != nil {
				continue
			}
			data, err := os.ReadFile(filepath.Join(toolDir, name, "manifest.json"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("reading legacy %s profile %q: %w", t.Name, name, err)
			}
			var manifest legacyProfileManifest
			if json.Unmarshal(data, &manifest) != nil || manifest.Spec == nil {
				continue
			}
			endpoint := t.ResolveEndpoint(strings.TrimSpace(manifest.Spec.Endpoint))
			key := strings.TrimSpace(manifest.Spec.Key)
			model := strings.TrimSpace(manifest.Spec.Model)
			slugs := legacyModelSlugs(t.Name, model, manifest.Spec.Models)
			if key == "" || model == "" || len(slugs) == 0 ||
				tools.ValidateKey(key) != nil || tools.ValidateEndpoint(endpoint) != nil {
				continue
			}

			baseName := name
			if baseName == "default" {
				baseName = "imported"
			}
			targetName := baseName
			for suffix := 2; ; suffix++ {
				existing, found, err := c.BindingByName(t.Name, targetName)
				if err != nil {
					return err
				}
				if !found {
					break
				}
				if c.bindingMatchesLegacy(existing, endpoint, key, model, slugs) {
					imported[t.Name+"\x00"+name] = existing
					targetName = ""
					break
				}
				targetName = fmt.Sprintf("%s-%d", baseName, suffix)
			}
			if targetName == "" {
				continue
			}

			p, err := c.PutProvider(endpoint)
			if err != nil {
				return err
			}
			cr, err := c.PutCredential(p.ID, key)
			if err != nil {
				return err
			}
			for _, slug := range slugs {
				if _, err := c.PutModel(p.ID, slug); err != nil {
					return err
				}
			}
			b, err := c.AddBinding(t.Name, targetName, cr.ID, model, slugs)
			if err != nil {
				// Another process may have imported this same profile while this one
				// was writing its rows. Reuse it only if its content matches.
				if existing, found, lookupErr := c.BindingByName(t.Name, targetName); lookupErr == nil && found &&
					c.bindingMatchesLegacy(existing, endpoint, key, model, slugs) {
					b, err = existing, nil
				}
				if err != nil {
					return err
				}
			}
			imported[t.Name+"\x00"+name] = b
		}
	}

	// The old config.json only tracked names. Translate those pointers to binding ids;
	// it remains on disk until charon itself next writes its new settings there.
	if err := c.lock(); err != nil {
		return err
	}
	defer c.unlock()
	active, err := c.readActive()
	if err != nil {
		return err
	}
	changed := false
	for tool, name := range legacyCfg.Active {
		if b, ok := imported[tool+"\x00"+name]; ok {
			if _, exists := active[tool]; !exists {
				active[tool] = b.ID
				changed = true
			}
		}
	}
	if changed {
		if err := c.writeActive(active); err != nil {
			return err
		}
	}

	return artifact.AtomicWrite(marker, []byte("1\n"), 0o600)
}

func legacyModelSlugs(tool, model string, models []string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		models = cleanSlugs(models)
		if len(models) > 0 {
			model = models[0]
		}
	}
	if model == "" {
		return nil
	}
	if SingleModelTools[tool] {
		return []string{model}
	}
	return cleanSlugs(append(append([]string{}, models...), model))
}

func (c *Catalog) bindingMatchesLegacy(b Binding, endpoint, key, model string, models []string) bool {
	cr, err := c.Credential(b.CredentialID)
	if err != nil || strings.TrimSpace(cr.Key) != key {
		return false
	}
	p, err := c.Provider(cr.ProviderID)
	if err != nil || strings.TrimRight(p.BaseURL, "/") != strings.TrimRight(endpoint, "/") {
		return false
	}
	slug, err := c.ModelSlug(b.ModelID)
	if err != nil || slug != model {
		return false
	}
	slugs, err := c.ModelSlugs(b.Models)
	return err == nil && slices.Equal(slugs, models)
}
