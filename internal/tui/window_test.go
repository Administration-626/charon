package tui

import (
	"fmt"
	"strings"
	"testing"

	"charon/internal/models"

	tea "github.com/charmbracelet/bubbletea"
)

// TestWindowPresetsOfferTypicalValues checks the chooser lists the presets plus
// the two escape hatches, and opens on the row matching the stored window.
func TestWindowPresetsOfferTypicalValues(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://gateway.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := st.PutModelWithWindow(p.ID, "glm-4.7", 200_000)
	if err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.editingModel = stored
	m.modelEditEndpoint = p.BaseURL
	m.openWindowPresets(false)

	if m.view != viewWindowPresets {
		t.Fatalf("view = %v, want viewWindowPresets", m.view)
	}
	if got, want := len(m.list.Items()), len(contextWindowPresets)+2; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	for _, preset := range contextWindowPresets {
		found := false
		for _, raw := range m.list.Items() {
			if it, ok := raw.(item); ok && it.value == fmt.Sprint(preset.tokens) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("preset %s (%d) missing from the list", preset.label, preset.tokens)
		}
	}
	values := []string{}
	for _, raw := range m.list.Items() {
		if it, ok := raw.(item); ok {
			values = append(values, it.value)
		}
	}
	if got := values[len(values)-2]; got != windowCustomSentinel {
		t.Errorf("second-to-last row = %q, want the Custom… sentinel", got)
	}
	if got := values[len(values)-1]; got != "0" {
		t.Errorf("last row = %q, want the Unknown/clear row", got)
	}
	if it, ok := m.list.SelectedItem().(item); !ok || it.value != "200000" {
		t.Fatalf("cursor = %+v, want the 200K preset", it)
	}
	if !strings.Contains(m.list.Title, "current 200,000 (manual)") {
		t.Errorf("title = %q, want the current window and its source", m.list.Title)
	}

	// Unknown clears the stored window and drops the manual source.
	m.list.Select(indexOfValue(m.list.Items(), "0"))
	next, _ := m.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.view != viewModels {
		t.Fatalf("view = %v, want viewModels", got.view)
	}
	cleared, err := st.Model(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ContextWindow != 0 || cleared.ContextWindowSource != "" {
		t.Fatalf("cleared model = %+v, want no window and no source", cleared)
	}
}

// TestWindowPresetsCancelReturns checks esc lands back on the screen that opened
// the chooser: the library, the picker, or the review table.
func TestWindowPresetsCancelReturns(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://gateway.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := st.PutModelWithWindow(p.ID, "glm-4.7", 200_000)
	if err != nil {
		t.Fatal(err)
	}

	lib := newModel(st, "test")
	lib.editingModel = stored
	lib.modelEditEndpoint = p.BaseURL
	lib.openWindowPresets(false)
	next, _ := lib.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEsc})
	afterLib := next.(model)
	if afterLib.view != viewModels {
		t.Fatalf("library esc: view = %v, want viewModels", afterLib.view)
	}
	if afterLib.editingModel.ID != "" || afterLib.modelEditEndpoint != "" {
		t.Fatalf("library esc left the edit state behind: %+v %q", afterLib.editingModel, afterLib.modelEditEndpoint)
	}

	picker := pickerModel(t, []string{"glm-4.6"})
	picker.editTarget = "glm-4.6"
	picker.openWindowPresets(true)
	next, _ = picker.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEsc})
	afterPicker := next.(model)
	if afterPicker.view != viewPickModel {
		t.Fatalf("picker esc: view = %v, want viewPickModel", afterPicker.view)
	}
	if afterPicker.editTarget != "" {
		t.Fatalf("picker esc left editTarget = %q", afterPicker.editTarget)
	}

}

// TestWindowPresetsPrefillFromCatalog covers the wizard path: a model the
// endpoint already stores shows its window as current, and picking a preset
// marks it manual without going through the text field.
func TestWindowPresetsPrefillFromCatalog(t *testing.T) {
	m := pickerModel(t, []string{"glm-4.6"})
	p, err := m.cat.PutProvider(m.tool.ResolveEndpoint(""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.cat.PutModelWithWindow(p.ID, "glm-4.6", 128_000); err != nil {
		t.Fatal(err)
	}

	m.editTarget = "glm-4.6"
	m.openWindowPresets(true)
	if got := m.windowEditValue(); got != 128_000 {
		t.Fatalf("current = %d, want the stored 128000", got)
	}
	if it, ok := m.list.SelectedItem().(item); !ok || it.value != "128000" {
		t.Fatalf("cursor = %+v, want the 128K preset", it)
	}

	m.list.Select(indexOfValue(m.list.Items(), "400000"))
	next, _ := m.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.view != viewPickModel {
		t.Fatalf("view = %v, want viewPickModel", got.view)
	}
	if got.wiz.windows["glm-4.6"] != 400_000 {
		t.Errorf("window = %d, want 400000", got.wiz.windows["glm-4.6"])
	}
	if !got.wiz.manualWindows["glm-4.6"] {
		t.Error("preset choice must be recorded as manual")
	}
	if got.editTarget != "" {
		t.Errorf("editTarget = %q after applying the preset", got.editTarget)
	}
}

// TestWindowPresetsCustomOpensTextEditor checks Custom… hands the current value
// to the text field rather than starting empty.
func TestWindowPresetsCustomOpensTextEditor(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://gateway.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := st.PutModelWithWindow(p.ID, "glm-4.7", 350_000)
	if err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.editingModel = stored
	m.modelEditEndpoint = p.BaseURL
	m.openWindowPresets(false)
	m.list.Select(indexOfValue(m.list.Items(), windowCustomSentinel))
	next, _ := m.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.view != viewModelWindow {
		t.Fatalf("view = %v, want viewModelWindow", got.view)
	}
	if got.input.Value() != "350000" {
		t.Fatalf("input = %q, want the current 350000 prefilled", got.input.Value())
	}
}

// TestWindowPresetsKeepUncoveredValue checks a value no preset covers becomes a
// selected "Keep current" row, so enter closes the chooser without writing and
// without marking a builtin as manual.
func TestWindowPresetsKeepUncoveredValue(t *testing.T) {
	m := pickerModel(t, []string{"z-ai/glm-5.3"})
	slug := "z-ai/glm-5.3"
	m.wiz.windows[slug] = models.DefaultContextCeiling // 600K, not a preset
	m.editTarget = slug
	m.openWindowPresets(true)

	if got, ok := m.list.Items()[0].(item); !ok || got.value != windowKeepSentinel {
		t.Fatalf("first row = %+v, want Keep current", got)
	}
	if it, ok := m.list.SelectedItem().(item); !ok || it.value != windowKeepSentinel {
		t.Fatalf("cursor = %+v, want Keep current", it)
	}
	if got := m.list.Items()[0].(item).title; !strings.Contains(got, "Keep current · 600,000 tokens · builtin") {
		t.Errorf("keep row = %q, want the value and source", got)
	}
	if !strings.Contains(m.list.Title, "current 600,000 (builtin)") {
		t.Errorf("title = %q, want the current value and source", m.list.Title)
	}

	next, _ := m.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.view != viewPickModel {
		t.Fatalf("view = %v, want viewPickModel", got.view)
	}
	if got.wiz.windows[slug] != models.DefaultContextCeiling {
		t.Errorf("window = %d, want it kept at %d", got.wiz.windows[slug], models.DefaultContextCeiling)
	}
	if got.wiz.manualWindows[slug] {
		t.Error("keeping a builtin must not mark it manual")
	}
	if !strings.Contains(got.status, "kept context window") {
		t.Errorf("status = %q, want the kept confirmation", got.status)
	}
	if got.editTarget != "" {
		t.Errorf("editTarget = %q after keeping", got.editTarget)
	}
}

// TestWindowPresetsTypedDigitStartsCustom checks the list is not a detour for
// someone who knows the number: a digit opens the text field with it carried over.
func TestWindowPresetsTypedDigitStartsCustom(t *testing.T) {
	m := pickerModel(t, []string{"glm-4.6"})
	m.editTarget = "glm-4.6"
	m.openWindowPresets(true)

	next, _ := m.updateWindowPresets(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	got := next.(model)
	if got.view != viewEditField {
		t.Fatalf("view = %v, want viewEditField", got.view)
	}
	if got.input.Value() != "2" {
		t.Fatalf("input = %q, want the typed digit carried over", got.input.Value())
	}
}

// TestWindowPresetsConfirmCurrentIsNoOp checks that enter on the row matching
// the current value keeps its source: confirming a builtin must not freeze it as
// a manual override.
func TestWindowPresetsConfirmCurrentIsNoOp(t *testing.T) {
	m := pickerModel(t, []string{"glm-4.6"})
	slug := "glm-4.6"
	m.wiz.windows[slug] = 200_000
	m.editTarget = slug
	m.openWindowPresets(true)

	m.list.Select(indexOfValue(m.list.Items(), "200000"))
	next, _ := m.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.wiz.windows[slug] != 200_000 {
		t.Fatalf("window = %d, want it kept", got.wiz.windows[slug])
	}
	if got.wiz.manualWindows[slug] {
		t.Error("confirming the current preset must not mark it manual")
	}
	if got.view != viewPickModel {
		t.Fatalf("view = %v, want viewPickModel", got.view)
	}
}
