package tui

import (
	"strings"
	"testing"

	"charon/internal/tools"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInteractiveFlowVisualAudit(t *testing.T) {
	st := openTestCatalog(t, false)

	tool := &tools.Tool{Name: "claude", Title: "Claude Code", ModelMenu: "/model"}
	m := newModel(st, "v1.3.12")
	m.width = 100
	m.height = 30
	m.resize()
	m.tool = tool
	m.wiz = wizard{
		name:     "test-binding",
		endpoint: "https://api.example.com/v1",
		key:      "sk-test",
		models:   []string{"claude-3-5-sonnet", "claude-3-haiku"},
	}
	m.view = viewEditForm
	m.loadEditForm()

	// Step 1: Form View Inspection
	formView := m.View()
	if !strings.Contains(formView, "Fetch & Pick Online Models") {
		t.Fatal("Form missing Fetch & Pick Online Models button")
	}
	if !strings.Contains(formView, "Type Model IDs Manually") {
		t.Fatal("Form missing Type Model IDs Manually button")
	}
	if !strings.Contains(formView, "offered in /model") {
		t.Fatal("Form missing /model menu note")
	}

	// Step 2: Open Manual Entry
	m.formFocus = focusManual
	next, _ := m.updateEditForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	manualView := m.View()
	if m.view != viewAddCustomModel {
		t.Fatalf("view = %v, want viewAddCustomModel", m.view)
	}
	if !strings.Contains(manualView, "claude-3-5-sonnet, claude-3-haiku") {
		t.Fatal("Manual entry did not prefill existing models")
	}

	// Step 3: Model Picker View
	m.view = viewPickModel
	m.wiz.models = nil
	m.wiz.model = ""
	allModels := []string{"claude-3-5-sonnet", "claude-3-opus", "claude-3-haiku"}
	m.showModels(allModels)

	pickerView := m.View()
	if !strings.Contains(pickerView, "ctrl+a selects all") {
		t.Fatal("Picker missing ctrl+a in tip")
	}

	// Step 4: Press Space on first model
	idx := indexOfValue(m.list.Items(), "claude-3-5-sonnet")
	m.list.Select(idx)
	next, _ = m.updatePickModel(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(model)
	afterSpaceView := m.View()
	if !strings.Contains(afterSpaceView, "Done — register these 1 model(s)") {
		t.Fatal("Picker missing Done row after Space")
	}

	// Step 5: Press Ctrl+A to select all
	next, _ = m.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	m = next.(model)
	afterCtrlAView := m.View()
	if !strings.Contains(afterCtrlAView, "Done — register these 3 model(s)") {
		t.Fatal("Picker missing Done row for all 3 models after Ctrl+A")
	}

	// Step 6: Press Ctrl+A again to deselect all
	next, _ = m.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	m = next.(model)
	afterDeselectView := m.View()
	if strings.Contains(afterDeselectView, "Done — register these") {
		t.Fatal("Done row should disappear after clearing all selections")
	}

	// Step 7: Press Enter on Done row when models are selected
	m.wiz.models = []string{"claude-3-5-sonnet", "claude-3-opus"}
	m.renderModels()
	m.list.Select(0) // Done row
	next, _ = m.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.view != viewEditForm {
		t.Fatalf("view = %v, want viewEditForm after Enter on Done", m.view)
	}

	// Step 8: Codex rejection and clean single-model view check
	mCodex := newModel(st, "v1.3.12")
	mCodex.width = 100
	mCodex.height = 30
	mCodex.resize()
	mCodex.tool = &tools.Tool{Name: "codex", Title: "Codex", ModelMenu: ""}
	mCodex.view = viewPickModel
	mCodex.wiz.models = []string{"gpt-5.5", "gpt-5.4"}
	mCodex.wiz.model = "gpt-5.5"
	mCodex.showModels([]string{"gpt-5.5", "gpt-5.4"})
	mCodex.list.Select(indexOfValue(mCodex.list.Items(), "gpt-5.5"))

	codexView := mCodex.View()
	if strings.Contains(codexView, "Done — register") {
		t.Fatal("Codex must never show Done row")
	}
	if strings.Contains(codexView, "• gpt") {
		t.Fatal("Codex must never show bullet selection marks on model rows")
	}
	if !strings.Contains(codexView, "Codex only supports a single model") {
		t.Fatal("Codex tip must state it only supports a single model")
	}

	next, _ = mCodex.updatePickModel(tea.KeyMsg{Type: tea.KeySpace})
	mCodex = next.(model)
	if !strings.Contains(mCodex.status, "can't be given a model list") {
		t.Fatal("Codex did not reject Space")
	}

	next, _ = mCodex.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	mCodex = next.(model)
	if !strings.Contains(mCodex.status, "can't be given a model list") {
		t.Fatal("Codex did not reject Ctrl+A")
	}
}
