package tui

import (
	"fmt"

	"charon/internal/catalog"
	"charon/internal/models"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The chooser's synthetic rows. Each is consumed by updateWindowPresets before
// any sentinel guard can see it, so they never leak into another screen.
const (
	windowCustomSentinel = "\x00custom-window" // type an exact count
	windowKeepSentinel   = "\x00keep-window"   // accept the value already in place
)

// windowPreset is one offered context size. The label is what the row shows and
// tokens is exactly what gets written, so the two are always rendered together.
type windowPreset struct {
	label  string
	tokens int
}

// contextWindowPresets covers the sizes the mainstream providers actually ship:
// OpenAI's 128K/400K, Anthropic's 200K/1M, Gemini's 256K/1M, and charon's own
// 500K fallback. Odd vendor numbers stay reachable through Custom….
var contextWindowPresets = []windowPreset{
	{"32K", 32_768},
	{"64K", 65_536},
	{"128K", 128_000},
	{"200K", 200_000},
	{"256K", 262_144},
	{"400K", 400_000},
	{"500K", 500_000},
	{"1M", 1_000_000},
	{"2M", 2_000_000},
}

// openWindowPresets shows the typical context windows for the model being edited.
// wizardEdit selects the binding wizard's pending windows (editTarget) instead of
// a catalog model row.
func (m *model) openWindowPresets(wizardEdit bool) {
	m.windowPresetWizard = wizardEdit
	m.view = viewWindowPresets
	m.clearStatus()

	current, source := m.windowEditState()
	items := make([]list.Item, 0, len(contextWindowPresets)+3)
	selected := len(contextWindowPresets) // Custom… when no preset matches
	matched := false
	for _, preset := range contextWindowPresets {
		if preset.tokens == current {
			matched = true
		}
	}
	if current > 0 && !matched {
		// An uncovered value (a builtin 600K, an odd vendor count) becomes a real,
		// selected row: enter must never edit or clear it by surprise.
		items = append(items, item{
			title:  fmt.Sprintf("Keep current · %s tokens · %s", formatTokens(current), source),
			value:  windowKeepSentinel,
			active: true,
		})
		selected = 0
	}
	for _, preset := range contextWindowPresets {
		title := fmt.Sprintf("%-4s · %s tokens", preset.label, formatTokens(preset.tokens))
		if preset.tokens == current {
			title += "  ← current"
			selected = len(items)
		}
		items = append(items, item{title: title, value: fmt.Sprint(preset.tokens), active: preset.tokens == current})
	}
	items = append(items,
		item{title: "Custom… · type an exact token count", value: windowCustomSentinel},
		item{title: "Unknown · clear the context window", value: "0"},
	)
	if current == 0 {
		selected = len(items) - 1 // an unset window is already unknown
	}

	m.list.SetItems(items)
	m.list.Select(selected)
	title := "Context window — " + m.windowEditName()
	if current > 0 {
		title += fmt.Sprintf(" · current %s (%s)", formatTokens(current), source)
	}
	m.list.Title = title
	m.setFooterKeys(keyChoose, keyMove, keyTypeCustom, keyEsc)
	m.setDelegate(themedCompactDelegate())
}

// windowEditName is the model slug the presets are editing, for the title.
func (m model) windowEditName() string {
	if m.windowPresetWizard {
		return m.editTarget
	}
	if m.editingModel.Slug != "" {
		return m.editingModel.Slug
	}
	return "model"
}

// windowEditValue is the window the editor starts from, without its source.
func (m model) windowEditValue() int {
	value, _ := m.windowEditState()
	return value
}

// windowEditState returns the window the editor should show as current and a
// short source label: manual, builtin, fallback, or unknown. The wizard's pending
// value wins, then the catalog row the endpoint stores; a catalog model carries
// its own stored source.
func (m model) windowEditState() (int, string) {
	if m.windowPresetWizard {
		slug := m.editTarget
		if m.wiz.manualWindows[slug] {
			return m.wiz.windows[slug], "manual" // an explicit clear stays cleared
		}
		if w, ok := m.wiz.windows[slug]; ok && w > 0 {
			return w, windowSourceLabel(slug, catalog.WindowBuiltin)
		}
		if stored, ok := m.catalogWindow(slug); ok && stored.ContextWindow > 0 {
			return stored.ContextWindow, windowSourceLabel(stored.Slug, stored.ContextWindowSource)
		}
		return 0, "unknown"
	}
	stored := m.editingModel
	if stored.ContextWindow > 0 {
		return stored.ContextWindow, windowSourceLabel(stored.Slug, stored.ContextWindowSource)
	}
	return 0, "unknown"
}

// catalogWindow finds the catalog row for slug at the wizard's endpoint.
func (m model) catalogWindow(slug string) (catalog.Model, bool) {
	if m.cat == nil || m.tool == nil {
		return catalog.Model{}, false
	}
	p, found, err := m.cat.ProviderByURL(m.tool.ResolveEndpoint(m.wiz.endpoint))
	if err != nil || !found {
		return catalog.Model{}, false
	}
	ms, err := m.cat.ModelsForProvider(p.ID)
	if err != nil {
		return catalog.Model{}, false
	}
	for _, stored := range ms {
		if stored.Slug == slug {
			return stored, true
		}
	}
	return catalog.Model{}, false
}

// windowSourceLabel names where a window came from; a builtin-pattern slug with
// no verified rule is charon's 500K fallback, not a vendor figure.
func windowSourceLabel(slug string, source catalog.WindowSource) string {
	switch source {
	case catalog.WindowManual:
		return "manual"
	case catalog.WindowBuiltin:
		if models.IsKnownBuiltin(slug) {
			return "builtin"
		}
		return "fallback"
	}
	if source == "" {
		return "saved"
	}
	return string(source)
}

// updateWindowPresets drives the chooser: enter applies the highlighted row (a
// preset, Keep current, Custom…, or Unknown/clear), a typed digit jumps straight
// into the text editor, esc goes back.
func (m model) updateWindowPresets(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.cancelWindowPresets(), nil
	case tea.KeyEnter:
		it, ok := m.list.SelectedItem().(item)
		if !ok {
			return m, nil
		}
		switch it.value {
		case windowKeepSentinel:
			return m.keepWindow(), nil
		case windowCustomSentinel:
			return m.openWindowInput("")
		case "0":
			return m.applyWindow(0)
		}
		var tokens int
		if _, err := fmt.Sscan(it.value, &tokens); err != nil || tokens <= 0 {
			return m, nil
		}
		return m.applyWindow(tokens)
	case tea.KeyRunes:
		// Typing "200k" starts in the text field with the first digit carried over,
		// so the list is never a detour for someone who knows the number.
		if len(msg.Runes) == 1 && msg.Runes[0] >= '0' && msg.Runes[0] <= '9' {
			return m.openWindowInput(string(msg.Runes))
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// cancelWindowPresets abandons the edit and returns to the screen that opened it.
func (m model) cancelWindowPresets() model {
	m.clearStatus()
	if m.windowPresetWizard {
		m.editTarget = ""
		m.editField = ""
		m.view = viewPickModel
		m.renderModels()
		return m
	}
	targetID := m.editingModel.ID
	m.editingModel = catalog.Model{}
	m.modelEditEndpoint = ""
	m.view = viewModels
	m.loadModels(targetID)
	return m
}

// keepWindow closes the chooser without writing: the value is already in place,
// so keeping it is a cancel that reports what was kept.
func (m model) keepWindow() model {
	value := m.windowEditValue()
	name := m.windowEditName()
	m = m.cancelWindowPresets()
	if value > 0 {
		m.setStatus(statusOK, fmt.Sprintf("kept context window for %s: %d", name, value))
		return m
	}
	m.setStatus(statusOK, "kept context window unchanged")
	return m
}

// openWindowInput is the Custom… row (or the first typed digit): it hands the
// edit to the text field, prefilled with the seed or the current value so a small
// correction does not retype it.
func (m model) openWindowInput(seed string) (tea.Model, tea.Cmd) {
	if m.windowPresetWizard {
		m.view = viewEditField
		m.editField = ""
	} else {
		m.view = viewModelWindow
	}
	m.startInput("context window in tokens (e.g. 1m, 200k, 1048576, empty=unknown)", false)
	if seed == "" {
		if w := m.windowEditValue(); w > 0 {
			seed = fmt.Sprint(w)
		}
	}
	if seed != "" {
		m.input.SetValue(seed)
		m.input.CursorEnd()
	}
	return m, textinput.Blink
}

// applyWindow routes a chosen size to the binding wizard or the catalog.
// Confirming the value that is already there is a no-op, so a builtin never
// silently becomes a manual override just because the user pressed enter on its
// row. The model-library add flow still writes: its whole purpose is to create
// the row, even when the window stays unknown.
func (m model) applyWindow(tokens int) (tea.Model, tea.Cmd) {
	if current, _ := m.windowEditState(); tokens == current && (m.windowPresetWizard || m.editingModel.ID != "") {
		return m.keepWindow(), nil
	}
	if m.windowPresetWizard {
		return m.applyWizardWindow(m.editTarget, tokens), nil
	}
	return m.saveCatalogWindow(tokens)
}

// applyWizardWindow stores a pending window for the binding wizard's model and
// returns to the screen the editor came from. Zero clears the window; the manual
// flag keeps the picker from restoring a builtin later.
func (m model) applyWizardWindow(slug string, window int) model {
	if m.wiz.windows == nil {
		m.wiz.windows = map[string]int{}
	}
	if m.wiz.manualWindows == nil {
		m.wiz.manualWindows = map[string]bool{}
	}
	m.wiz.windows[slug] = window
	m.wiz.manualWindows[slug] = true
	m.editTarget = ""
	m.editField = ""
	m.clearStatus()
	m.view = viewPickModel
	m.renderModels()
	m.list.Select(indexOfValue(m.list.Items(), slug))
	m.setWindowStatus(slug, window)
	return m
}

// setWindowStatus reports a saved or cleared window in the footer.
func (m *model) setWindowStatus(slug string, window int) {
	if window == 0 {
		m.setStatus(statusOK, "cleared context window for "+slug)
		return
	}
	m.setStatus(statusOK, fmt.Sprintf("saved context window for %s: %d", slug, window))
}

// saveCatalogWindow upserts a model-library row with the chosen window. It is the
// save half of both the presets and the text editor, so the two cannot drift.
func (m model) saveCatalogWindow(window int) (tea.Model, tea.Cmd) {
	p, err := m.cat.PutProvider(m.modelEditEndpoint)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	saved, err := m.cat.PutModelWithWindow(p.ID, m.editingModel.Slug, window)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	if m.editingModel.ID != "" && m.editingModel.ID != saved.ID {
		if err := m.cat.RemoveModel(m.editingModel.ID); err != nil {
			m.setStatus(statusErr, err.Error())
			m.view = viewModels
			m.loadModels(saved.ID)
			return m, nil
		}
	}
	m.editingModel = catalog.Model{}
	m.modelEditEndpoint = ""
	m.view = viewModels
	m.loadModels(saved.ID)
	m.setStatus(statusOK, "Saved "+saved.Slug)
	return m, nil
}
