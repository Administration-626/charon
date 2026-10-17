package catalog

import (
	"fmt"
	"sort"
	"strings"

	"charon/internal/tools"
)

// ProjectIfActive re-renders the latest stored version of id only when its tool
// still points to it as active. The check and write share the same lock as switch.
// Bindings for tools no longer supported are kept but never projected.
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
	if active[b.Tool] != id || tools.Find(b.Tool) == nil {
		return false, nil
	}
	if err := c.project(b); err != nil {
		return false, err
	}
	return true, nil
}

// ProjectSpec renders binding b into the AuthSpec its tool would receive,
// without writing anything: endpoint and key come from the credential's
// provider, the default model and the picker list from the models the
// binding references. Dry-run previews run this through a sandboxed copy.
func (c *Catalog) ProjectSpec(b Binding) (tools.AuthSpec, error) {
	cr, err := c.Credential(b.CredentialID)
	if err != nil {
		return tools.AuthSpec{}, err
	}
	p, err := c.Provider(cr.ProviderID)
	if err != nil {
		return tools.AuthSpec{}, err
	}
	slug, err := c.ModelSlug(b.ModelID)
	if err != nil {
		return tools.AuthSpec{}, err
	}
	spec := tools.AuthSpec{Endpoint: p.BaseURL, Key: cr.Key, Model: slug, PiAPI: b.PiAPI}
	officialOpenAI := tools.IsOfficialOpenAIEndpoint(p.BaseURL)
	specs := make([]tools.ModelSpec, 0, len(b.Models))
	for _, id := range b.Models {
		m, err := c.Model(id)
		if err != nil {
			return tools.AuthSpec{}, err
		}
		window := m.ContextWindow
		// Codex's own model catalog applies only at OpenAI's endpoint. A gateway
		// can reuse a GPT-shaped id while Codex falls back to about 258K.
		if b.Tool == "codex" && officialOpenAI && len(b.Models) == 1 && m.ContextWindowSource == WindowBuiltin {
			window = 0
		}
		specs = append(specs, tools.ModelSpec{Slug: m.Slug, ContextWindow: window, Effort: m.Effort, ThinkingLevelMap: m.ThinkingLevelMap})
		if id == b.ModelID {
			spec.Effort = m.Effort
			if spec.Effort == "" {
				spec.Effort = "medium"
			}
		}
	}
	spec.Models = specs
	// A tool whose config holds several endpoints at once also registers the
	// tool's other bindings, one entry per endpoint, so its picker can switch
	// between them without charon. Only bindings of this same tool qualify: a
	// catalog endpoint another tool uses was never given a model list or a
	// request protocol for this one, and a hidden binding stays out on purpose.
	if t := tools.Find(b.Tool); t != nil && t.MultiEndpoint {
		groups, err := c.endpointGroups(b.Tool, b.ID)
		if err != nil {
			return tools.AuthSpec{}, err
		}
		spec.Groups = groups
	}
	return spec, nil
}

// endpointGroups collects the endpoints a multi-endpoint tool registers next to
// the one its binding points at: one group per other visible binding of that
// same tool, carrying the key, model list and request protocol that binding
// saved. Only that tool's own bindings qualify — an endpoint another tool
// configured was never given a key, a picker list or a protocol for this one —
// and hidden bindings are skipped. The binding being rendered is left out (it is
// the primary entry), and so is any other binding on the same endpoint. When
// several bindings share an endpoint, the first visible one in file order stands
// for it, so the rendered set does not depend on table order; groups are ordered
// by endpoint URL for the same reason.
func (c *Catalog) endpointGroups(tool, excludeBindingID string) ([]tools.EndpointGroup, error) {
	bs, err := c.bindings()
	if err != nil {
		return nil, err
	}
	cs, err := c.credentials()
	if err != nil {
		return nil, err
	}
	ps, err := c.providers()
	if err != nil {
		return nil, err
	}
	ms, err := c.models()
	if err != nil {
		return nil, err
	}
	creds := make(map[string]Credential, len(cs))
	for _, cr := range cs {
		creds[cr.ID] = cr
	}
	providers := make(map[string]Provider, len(ps))
	for _, p := range ps {
		providers[p.ID] = p
	}
	models := make(map[string]Model, len(ms))
	for _, m := range ms {
		models[m.ID] = m
	}

	// The rendered binding's endpoint is the primary entry. Another binding of
	// the same endpoint (a rotated key, a different default model) must not
	// become a second entry for an endpoint that is already registered.
	primaryEndpoint := ""
	if idx := indexBinding(bs, excludeBindingID); idx >= 0 {
		if cr, ok := creds[bs[idx].CredentialID]; ok {
			primaryEndpoint = providers[cr.ProviderID].BaseURL
		}
	}

	seen := make(map[string]bool, len(bs))
	var groups []tools.EndpointGroup
	for _, b := range bs {
		// Hidden bindings are skipped before the per-endpoint bookkeeping, so a
		// hidden first binding cannot shadow the visible one for the same
		// endpoint.
		if b.Tool != tool || b.ID == excludeBindingID || b.Hidden {
			continue
		}
		cr, ok := creds[b.CredentialID]
		if !ok || cr.Key == "" {
			continue
		}
		p, ok := providers[cr.ProviderID]
		if !ok || p.BaseURL == "" || p.BaseURL == primaryEndpoint || seen[p.BaseURL] {
			continue
		}
		specs := make([]tools.ModelSpec, 0, len(b.Models))
		for _, id := range b.Models {
			m, ok := models[id]
			if !ok || strings.TrimSpace(m.Slug) == "" {
				continue
			}
			specs = append(specs, tools.ModelSpec{
				Slug:             m.Slug,
				ContextWindow:    m.ContextWindow,
				Effort:           m.Effort,
				ThinkingLevelMap: m.ThinkingLevelMap,
			})
		}
		if len(specs) == 0 {
			continue
		}
		seen[p.BaseURL] = true
		groups = append(groups, tools.EndpointGroup{
			Endpoint: p.BaseURL,
			Key:      cr.Key,
			PiAPI:    b.PiAPI,
			Models:   specs,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Endpoint < groups[j].Endpoint })
	return groups, nil
}

// AggregateCounts reports how many endpoints and models a multi-endpoint tool
// would register: one entry per visible endpoint, carrying the models that
// endpoint's first visible binding saved. Hidden bindings are left out, so the
// number matches what the picker will hold. The TUI shows it as a hint; callers
// may ignore a read error and leave the hint out.
func (c *Catalog) AggregateCounts(tool string) (endpoints, models int, err error) {
	groups, err := c.endpointGroups(tool, "")
	if err != nil {
		return 0, 0, err
	}
	for _, g := range groups {
		models += len(g.Models)
	}
	return len(groups), models, nil
}

func (c *Catalog) project(b Binding) error {
	t := tools.Find(b.Tool)
	if t == nil {
		return fmt.Errorf("unknown tool %q", b.Tool)
	}
	spec, err := c.ProjectSpec(b)
	if err != nil {
		return err
	}
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
	if b.Hidden {
		// Switching to a hidden binding is an explicit request to use it, so it
		// rejoins the tool's list rather than staying out of a config it now
		// defaults to.
		b.Hidden = false
		bs[idx] = b
		if err := writeTable(c.table("bindings.json"), bs); err != nil {
			return Binding{}, err
		}
	}
	c.captureStash(b.Tool)
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
