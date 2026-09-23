package catalog

import (
	"fmt"

	"charon/internal/tools"
)

// project renders a binding into its tool's live config: endpoint and key come
// from the credential's provider, the default model and the picker list from the
// models the binding references. It reuses the tool's ApplyAuth, which overwrites
// only the charon-owned keys and leaves everything else in the file alone.
//
// The binding owns the complete picker list, including a single-model list.
// Passing it on every projection prevents models from the previously active
// endpoint leaking into this binding's picker.
// ProjectIfActive re-renders the latest stored version of id only when its tool
// still points to it as active. The check and write share the same lock as switch.
func (c *Catalog) ProjectIfActive(id string) (bool, error) {
	if err := c.lock(); err != nil {
		return false, err
	}
	defer c.unlock()
	bs, err := c.bindings()
	if err != nil {
		return false, err
	}
	idx := indexBinding(bs, id)
	if idx < 0 {
		return false, fmt.Errorf("binding %q: %w", id, ErrNotFound)
	}
	b := bs[idx]
	active, err := c.readActive()
	if err != nil {
		return false, err
	}
	if active[b.Tool] != id {
		return false, nil
	}
	if err := c.project(b); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Catalog) project(b Binding) error {
	t := tools.Find(b.Tool)
	if t == nil {
		return fmt.Errorf("unknown tool %q", b.Tool)
	}
	cr, err := c.Credential(b.CredentialID)
	if err != nil {
		return err
	}
	p, err := c.Provider(cr.ProviderID)
	if err != nil {
		return err
	}
	slug, err := c.ModelSlug(b.ModelID)
	if err != nil {
		return err
	}
	spec := tools.AuthSpec{Endpoint: p.BaseURL, Key: cr.Key, Model: slug}
	slugs, err := c.ModelSlugs(b.Models)
	if err != nil {
		return err
	}
	spec.AllModels = slugs
	return t.ApplyAuth(spec)
}

// Activate renders a binding into its tool, then marks it active. The lock spans
// both steps so another charon process cannot interleave a switch.
func (c *Catalog) Activate(id string) (Binding, error) {
	if err := c.lock(); err != nil {
		return Binding{}, err
	}
	defer c.unlock()
	bs, err := c.bindings()
	if err != nil {
		return Binding{}, err
	}
	idx := indexBinding(bs, id)
	if idx < 0 {
		return Binding{}, fmt.Errorf("binding %q: %w", id, ErrNotFound)
	}
	b := bs[idx]
	if err := c.project(b); err != nil {
		return Binding{}, err
	}
	if err := c.setActiveForTool(b.Tool, b.ID); err != nil {
		return Binding{}, err
	}
	return b, nil
}
