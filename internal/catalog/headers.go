package catalog

import (
	"fmt"
	"maps"

	"charon/internal/tools"
)

// SetBindingHeaders replaces a Pi binding's request headers. Values are literal;
// nil forgets the override, while an empty map explicitly clears all headers.
func (c *Catalog) SetBindingHeaders(id string, headers map[string]string) (Binding, error) {
	if err := tools.ValidateHeaders(headers); err != nil {
		return Binding{}, err
	}
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
	if b.Tool != "pi" {
		return Binding{}, fmt.Errorf("request headers are only supported for Pi")
	}
	b.Headers = maps.Clone(headers)
	active, err := c.readActive()
	if err != nil {
		return Binding{}, err
	}
	if active["pi"] == id {
		if _, err := c.ProjectSpec(b); err != nil {
			return Binding{}, err
		}
	}
	bs[idx] = b
	if err := writeTable(c.table("bindings.json"), bs); err != nil {
		return Binding{}, err
	}
	return b, nil
}
