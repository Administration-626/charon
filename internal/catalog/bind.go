package catalog

import (
	"fmt"
	"strings"

	"charon/internal/tools"
)

// StoreBinding writes the provider, credential, and model rows a binding needs, then
// adds or updates the binding itself. modelsExplicit is true when the caller passed a
// model list; otherwise an existing binding keeps its list, and a new one stores only
// the default slug.
func StoreBinding(cat *Catalog, t *tools.Tool, existing *Binding, name, endpoint, key, model string, modelList []string, modelsExplicit bool) (Binding, error) {
	if t == nil {
		return Binding{}, fmt.Errorf("tool is required")
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
		if SingleModelTools[t.Name] {
			slugs = []string{model}
		} else {
			slugs = append(slugs, model)
		}
	}
	slugs = cleanSlugs(slugs)
	if model == "" && len(slugs) > 0 {
		model = slugs[0]
	}
	if len(slugs) == 0 {
		return Binding{}, fmt.Errorf("a binding needs at least one model")
	}
	if SingleModelTools[t.Name] && len(slugs) > 1 {
		return Binding{}, fmt.Errorf("%s can register only one model, got %d", t.Name, len(slugs))
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
	}

	if existing == nil {
		return cat.AddBinding(t.Name, name, cr.ID, model, slugs)
	}
	return cat.UpdateBinding(existing.ID, name, cr.ID, model, slugs)
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
