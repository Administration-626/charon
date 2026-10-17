package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"charon/internal/artifact"
	"charon/internal/tools"
)

// The stash remembers what a tool's config held before charon first wrote to
// it, so `charon off` puts the file back rather than guessing. One entry per
// tool, taken on the tool's first switch and kept until off.

func (c *Catalog) readStash() (map[string]tools.ToolStash, error) {
	data, err := os.ReadFile(c.table("stash.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]tools.ToolStash{}, nil
		}
		return nil, err
	}
	var st map[string]tools.ToolStash
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("reading stash.json: %w", err)
	}
	if st == nil {
		st = map[string]tools.ToolStash{}
	}
	return st, nil
}

func (c *Catalog) writeStash(st map[string]tools.ToolStash) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return artifact.AtomicWrite(c.table("stash.json"), append(data, '\n'), 0o600)
}

// captureStash records the tool's pre-charon state when none is stashed yet.
// The caller holds the catalog lock. A capture that fails leaves no entry:
// off then removes charon's entries without restoring, never blocks a switch.
func (c *Catalog) captureStash(tool string) {
	st, err := c.readStash()
	if err != nil {
		return
	}
	if _, had := st[tool]; had {
		return
	}
	t := tools.Find(tool)
	if t == nil {
		return
	}
	st[tool] = t.CaptureStash()
	_ = c.writeStash(st)
}

// Off takes charon back out of a tool: every entry it owns is removed, the
// paths that held something before charon are restored as they were written,
// files charon created are deleted, the tool's active pointer is cleared, and
// its stash entry goes. The tool's config is otherwise left as it is now.
func (c *Catalog) Off(tool string) error {
	if err := c.lock(); err != nil {
		return err
	}
	defer c.unlock()
	t := tools.Find(tool)
	if t == nil {
		return fmt.Errorf("unknown tool %q", tool)
	}
	st, err := c.readStash()
	if err != nil {
		return err
	}
	pre := st[tool]
	if err := t.Off(pre); err != nil {
		return err
	}
	active, err := c.readActive()
	if err != nil {
		return err
	}
	if _, had := active[tool]; had {
		delete(active, tool)
		if err := c.writeActive(active); err != nil {
			return err
		}
	}
	if _, had := st[tool]; had {
		delete(st, tool)
		return c.writeStash(st)
	}
	return nil
}
