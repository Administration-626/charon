package catalog

import (
	"fmt"
	"strings"

	"charon/internal/tools"
)

func endpointIdentity(endpoint string) string {
	return strings.TrimRight(strings.TrimSpace(endpoint), "/")
}

func (c *Catalog) bindingEndpoint(b Binding) (string, error) {
	cr, err := c.Credential(b.CredentialID)
	if err != nil {
		return "", err
	}
	p, err := c.Provider(cr.ProviderID)
	if err != nil {
		return "", err
	}
	return endpointIdentity(p.BaseURL), nil
}

func (c *Catalog) endpointBindingIndexes(bs []Binding, b Binding) ([]int, error) {
	endpoint, err := c.bindingEndpoint(b)
	if err != nil {
		return nil, err
	}
	var indexes []int
	for i, other := range bs {
		if other.Tool != "pi" {
			continue
		}
		otherEndpoint, err := c.bindingEndpoint(other)
		if err != nil {
			return nil, err
		}
		if endpoint == otherEndpoint {
			indexes = append(indexes, i)
		}
	}
	return indexes, nil
}

// Existing mixed flags are treated as visible, never as permission to hide a
// currently used site. Mutations normalize that endpoint; opening data is read-only.
func (c *Catalog) inheritPiVisibility(bs []Binding, b *Binding) error {
	if b.Tool != "pi" {
		return nil
	}
	indexes, err := c.endpointBindingIndexes(bs, *b)
	if err != nil {
		return err
	}
	active, err := c.readActive()
	if err != nil {
		return err
	}
	hidden := b.Hidden
	if len(indexes) > 0 {
		hidden = true
	}
	for _, i := range indexes {
		if !bs[i].Hidden || bs[i].ID == active["pi"] {
			hidden = false
		}
	}
	if b.ID == active["pi"] {
		hidden = false
	}
	b.Hidden = hidden
	for _, i := range indexes {
		bs[i].Hidden = hidden
	}
	return nil
}

func (c *Catalog) setPiEndpointHidden(bs []Binding, idx int, hidden bool) error {
	b := bs[idx]
	indexes, err := c.endpointBindingIndexes(bs, b)
	if err != nil {
		return err
	}
	active, err := c.readActive()
	if err != nil {
		return err
	}
	if hidden {
		for _, i := range indexes {
			if bs[i].ID == active["pi"] {
				return fmt.Errorf("%q is on the active Pi endpoint: switch to another endpoint in the TUI first", b.Name)
			}
		}
	}
	for _, i := range indexes {
		bs[i].Hidden = hidden
	}
	endpoint, err := c.bindingEndpoint(b)
	if err != nil {
		return err
	}
	group := tools.EndpointGroup{Endpoint: endpoint}
	if !hidden {
		// Use the same first-visible-in-file-order representative as ordinary apply.
		// Read the proposed flags without temporarily writing them to the catalog.
		groups, err := c.endpointGroupsFromBindings("pi", "", bs)
		if err != nil {
			return err
		}
		found := false
		for _, g := range groups {
			if endpointIdentity(g.Endpoint) == endpoint {
				group = g
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no usable saved Pi binding for this endpoint")
		}
	}
	apply, err := tools.PreparePiEndpointVisibility(group, hidden)
	if err != nil {
		return err
	}
	c.captureStash("pi")
	// ponytail: atomic per file, not a multi-file transaction. Write config first
	// so a failed config write leaves h's saved intent unchanged and can be retried.
	if err := apply(); err != nil {
		return fmt.Errorf("updating Pi endpoint failed; saved visibility is unchanged, fix the cause and retry h: %w", err)
	}
	if err := writeTable(c.table("bindings.json"), bs); err != nil {
		return fmt.Errorf("pi endpoint updated, but saving visibility failed; fix the cause and retry h: %w", err)
	}
	return nil
}
