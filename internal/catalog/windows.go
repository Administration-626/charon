package catalog

import (
	"errors"
	"os"

	"charon/internal/artifact"
	"charon/internal/models"
)

// contextWindowBackfillMarker records that stored model rows have been reconciled
// with the builtin window table; see backfillContextWindows.
const contextWindowBackfillMarker = ".context-windows-backfilled"

// backfillContextWindows fills a builtin context window into model rows saved
// before charon recorded windows. Rows that already carry a window (manual or
// builtin) are left alone, so a manual override survives. The one-shot marker is
// what keeps a deliberate "unknown" (a cleared window) from being re-filled by a
// later open, so clearing a window stays possible.
func (c *Catalog) backfillContextWindows() error {
	marker := c.table(contextWindowBackfillMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := c.lock(); err != nil {
		return err
	}
	defer c.unlock()
	ms, err := c.models()
	if err != nil {
		return err
	}
	changed := false
	for i, m := range ms {
		if m.ContextWindow != 0 || m.ContextWindowSource != "" {
			continue
		}
		if w := models.DefaultContextWindow(m.Slug); w > 0 {
			ms[i].ContextWindow = w
			ms[i].ContextWindowSource = WindowBuiltin
			changed = true
		}
	}
	if changed {
		if err := writeTable(c.table("models.json"), ms); err != nil {
			return err
		}
	}
	return artifact.AtomicWrite(marker, []byte("1\n"), 0o600)
}
