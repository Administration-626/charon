package catalog

import (
	"fmt"

	"charon/internal/tools"
)

// ProjectIfActive re-renders the latest stored version of id only when its tool
// still points to it as active. The check and write share the same lock as switch.
// It renders a binding into its tool's live config: endpoint and key come from the
// credential's provider, the default model and the picker list from the models the
// binding references. It reuses the tool's ApplyAuth, which overwrites only the
// charon-owned keys and leaves everything else in the file alone.
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
	officialOpenAI := tools.IsOfficialOpenAIEndpoint(p.BaseURL)
	specs := make([]tools.ModelSpec, 0, len(b.Models))
	for _, id := range b.Models {
		m, err := c.Model(id)
		if err != nil {
			return err
		}
		window := m.ContextWindow
		// Codex's own model catalog applies only at OpenAI's endpoint. A gateway
		// can reuse a GPT-shaped id while Codex falls back to about 258K.
		if b.Tool == "codex" && officialOpenAI && len(b.Models) == 1 && m.ContextWindowSource == WindowBuiltin {
			window = 0
		}
		specs = append(specs, tools.ModelSpec{Slug: m.Slug, ContextWindow: window, Effort: m.Effort})
		if id == b.ModelID {
			spec.Effort = m.Effort
			if spec.Effort == "" {
				spec.Effort = "medium"
			}
		}
	}
	spec.Models = specs
	return t.ApplyAuth(spec)
}

// Activate renders a binding into its tool, then marks it active. The lock spans
// both steps so another charon process cannot interleave a switch.
func (c *Catalog) Activate(id string) (Binding, error) {
	return c.activate(id, false)
}

// Reapply renders a binding even when it is already the last applied one.
// Use it only for an explicit user request: rendering resets the tool's live
// model to the binding's saved default.
func (c *Catalog) Reapply(id string) (Binding, error) {
	return c.activate(id, true)
}

func (c *Catalog) activate(id string, reapply bool) (Binding, error) {
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
	active, err := c.readActive()
	if err != nil {
		return Binding{}, err
	}
	same := active[b.Tool] == id
	if same && !reapply {
		return b, nil
	}
	if err := c.project(b); err != nil {
		return Binding{}, err
	}
	if same {
		return b, nil
	}
	if err := c.setActiveForTool(b.Tool, id); err != nil {
		return Binding{}, fmt.Errorf("recording active %s binding after updating tool config (tool config may have changed): %w", b.Tool, err)
	}
	return b, nil
}
