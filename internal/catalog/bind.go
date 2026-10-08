package catalog

import (
	"fmt"
	"strings"

	"charon/internal/tools"
)

// StoreBinding writes the provider, credential, and model rows a binding needs, then
// adds or updates the binding itself. modelsExplicit is true when the caller passed a
// model list; otherwise an existing binding keeps its list, and a new one stores only
// the default slug. piAPI optionally sets Pi's request protocol.
func StoreBinding(cat *Catalog, t *tools.Tool, existing *Binding, name, endpoint, key, model string, modelList []string, modelsExplicit bool, manualWindows map[string]int, piAPI ...string) (Binding, error) {
	if t == nil {
		return Binding{}, fmt.Errorf("tool is required")
	}
	if _, err := bindingPiAPI(t.Name, "", piAPI); err != nil {
		return Binding{}, err
	}
	if existing == nil {
		if err := validateName(name); err != nil {
			return Binding{}, err
		}
	}
	if err := tools.ValidateKey(key); err != nil {
		return Binding{}, err
	}
	if err := tools.ValidateEndpoint(t.ResolveEndpoint(endpoint)); err != nil {
		return Binding{}, err
	}
	bs, err := cat.bindings()
	if err != nil {
		return Binding{}, err
	}
	if existing != nil {
		idx := indexBinding(bs, existing.ID)
		if idx < 0 {
			return Binding{}, fmt.Errorf("binding %q: %w", existing.ID, ErrNotFound)
		}
		if name != bs[idx].Name {
			if err := validateName(name); err != nil {
				return Binding{}, err
			}
		}
	}
	for _, b := range bs {
		if b.Tool == t.Name && b.Name == name && (existing == nil || b.ID != existing.ID) {
			return Binding{}, fmt.Errorf("a %s binding named %q already exists", t.Name, name)
		}
	}

	model = strings.TrimSpace(model)
	slugs := modelList
	if !modelsExplicit {
		if existing != nil {
			slugs, err = cat.ModelSlugs(existing.Models)
			if err != nil {
				return Binding{}, err
			}
		} else if model != "" {
			slugs = []string{model}
		}
	}
	if model != "" && !contains(slugs, model) {
		slugs = append(slugs, model)
	}
	slugs = cleanSlugs(slugs)
	if model == "" && len(slugs) > 0 {
		model = slugs[0]
	}
	if len(slugs) == 0 {
		return Binding{}, fmt.Errorf("a binding needs at least one model")
	}

	ep := t.ResolveEndpoint(endpoint)
	p, err := cat.PutProvider(ep)
	if err != nil {
		return Binding{}, err
	}
	cr, err := cat.PutCredential(p.ID, key)
	if err != nil {
		return Binding{}, err
	}
	for _, slug := range slugs {
		if _, err := cat.PutModel(p.ID, slug); err != nil {
			return Binding{}, err
		}
		if window, ok := manualWindows[slug]; ok {
			if _, err := cat.PutModelWithWindow(p.ID, slug, window); err != nil {
				return Binding{}, err
			}
		}
	}

	if existing == nil {
		return cat.AddBinding(t.Name, name, cr.ID, model, slugs, piAPI...)
	}
	return cat.UpdateBinding(existing.ID, name, cr.ID, model, slugs, piAPI...)
}

func bindingPiAPI(tool, current string, override []string) (string, error) {
	if len(override) > 1 {
		return "", fmt.Errorf("at most one Pi endpoint type is allowed")
	}
	if len(override) == 1 {
		current = override[0]
	}
	if tool != "pi" {
		if current != "" {
			return "", fmt.Errorf("endpoint type selection is only supported for Pi")
		}
		return "", nil
	}
	return tools.ResolvePiAPI(current)
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
