package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/tools"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPiEndpointDropdown(t *testing.T) {
	m := newModel(openTestCatalog(t, false), "test")
	m.tool, m.view = tools.Find("pi"), viewEditForm
	m.width, m.height = 80, 24
	m.wiz = wizard{name: "work", endpoint: "https://example.test/v1", key: "sk-not-for-display", model: "custom"}
	m.loadEditFormAt(focusPiAPI)
	if m.wiz.piAPI != "openai-completions" {
		t.Fatalf("default API = %q", m.wiz.piAPI)
	}
	next, _ := m.updateEditForm(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !m.piAPIOpen || m.piAPICursor != 0 {
		t.Fatal("enter did not open dropdown at saved API")
	}
	view := m.View()
	labels := []string{"OpenAI (Chat Completions)", "OpenAI (Responses)", "Anthropic (Messages)", "Gemini (Generate Content)"}
	if len(piAPIOptions) != len(labels) || strings.Contains(view, "Compact") {
		t.Fatal("dropdown must offer only the four supported protocols")
	}
	for i, label := range labels {
		if piAPIOptions[i].label != label || !strings.Contains(view, label) {
			t.Errorf("dropdown is missing %q", label)
		}
	}
	if strings.Count(view, "[✓]") != 1 || !strings.Contains(view, "> [✓] OpenAI (Chat Completions)") {
		t.Fatal("dropdown must mark the current choice separately from the cursor")
	}
	if strings.Contains(view, m.wiz.key) {
		t.Fatal("dropdown exposed API key")
	}
	if rows := len(strings.Split(view, "\n")); rows != m.height {
		t.Fatalf("dropdown has %d rows, want %d", rows, m.height)
	}
	next, _ = m.updatePiAPI(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	view = m.View()
	if strings.Count(view, "[✓]") != 1 || !strings.Contains(view, "[✓] OpenAI (Chat Completions)") ||
		!strings.Contains(view, "> [ ] OpenAI (Responses)") {
		t.Fatal("moving the cursor changed or hid the current choice marker")
	}
	next, _ = m.updatePiAPI(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.piAPIOpen || m.view != viewEditForm || m.wiz.name != "work" || m.wiz.piAPI != "openai-completions" {
		t.Fatal("escape should close only the dropdown and preserve the form")
	}
	for i, option := range piAPIOptions {
		if _, err := tools.ResolvePiAPI(option.api); err != nil || option.api == "" {
			t.Fatalf("dropdown contains unsupported API %q", option.api)
		}
		m.piAPIOpen, m.piAPICursor = true, i
		next, _ = m.updatePiAPI(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(model)
		if m.piAPIOpen || m.wiz.piAPI != option.api {
			t.Fatalf("selected API = %q, want %q", m.wiz.piAPI, option.api)
		}
		if !strings.Contains(m.status, option.label) || !strings.Contains(m.status, "Save") {
			t.Fatal("selection must confirm the choice without claiming the binding is saved")
		}
		m.formFocus = focusFetch
		m.applyFormFocus()
		if view := m.View(); !strings.Contains(view, option.label) || strings.Contains(view, "[✓]") {
			t.Fatal("collapsed form must show the chosen type after focus moves away")
		}
		m.formFocus = focusPiAPI
		m.applyFormFocus()
		next, _ = m.updateEditForm(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(model)
		if !m.piAPIOpen || m.piAPICursor != i {
			t.Fatal("reopening dropdown lost selection")
		}
		if view := m.View(); strings.Count(view, "[✓]") != 1 || !strings.Contains(view, "> [✓] "+option.label) {
			t.Fatal("reopening dropdown did not mark the confirmed choice")
		}
		next, _ = m.updatePiAPI(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(model)
		next, _ = m.updatePiAPI(tea.KeyMsg{Type: tea.KeyEsc})
		m = next.(model)
		if m.wiz.piAPI != option.api {
			t.Fatal("escape committed an unconfirmed selection")
		}
	}
	if hint := m.endpointHint("https://generativelanguage.googleapis.com"); strings.Contains(hint, "/v1") || !strings.Contains(hint, "manually") {
		t.Fatalf("wrong Gemini hint: %s", hint)
	}
}

func TestEndpointDropdownOnlyAppearsForPi(t *testing.T) {
	st := openTestCatalog(t, false)
	for _, tool := range tools.All() {
		t.Run(tool.Name, func(t *testing.T) {
			m := newModel(st, "test")
			m.tool, m.view = tool, viewEditForm
			m.loadEditFormAt(focusToken)
			m.moveFormFocus(1)
			want := focusFetch
			if tool.Name == "pi" {
				want = focusPiAPI
			}
			if m.formFocus != want {
				t.Fatalf("next focus = %d, want %d", m.formFocus, want)
			}
			m.moveFormFocus(-1)
			if m.formFocus != focusToken {
				t.Fatal("reverse navigation did not return to API key")
			}
			m.formFocus = focusCancel
			m.moveFormFocus(1)
			if m.formFocus != focusName {
				t.Fatal("form focus did not wrap")
			}
			if strings.Contains(m.View(), "Endpoint Type") != (tool.Name == "pi") {
				t.Fatal("endpoint type should appear only for Pi")
			}
		})
	}
}

func TestPiEndpointTypeSaveEditAndCopy(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "test")
	m.tool, m.view = tools.Find("pi"), viewEditForm
	m.width, m.height = 80, 30
	m.resize()
	m.wiz = wizard{name: "work", endpoint: "https://example.test", key: "sk-test", model: "custom", piAPI: "anthropic-messages"}
	m.loadEditForm()
	next, _ := m.submitForm()
	m = next.(model)
	b, found, err := st.BindingByName("pi", "work")
	if err != nil || !found || b.PiAPI != "anthropic-messages" || m.statusLvl != statusOK {
		t.Fatalf("save protocol = %q, err=%v, status=%s", b.PiAPI, err, m.status)
	}
	m.clearStatus() // The saved type must remain visible without the save notification.
	if !strings.Contains(m.View(), "Anthropic (Messages)") {
		t.Fatal("binding list did not show the saved endpoint type")
	}
	next, _ = m.onEditKey()
	m = next.(model)
	if m.wiz.piAPI != b.PiAPI {
		t.Fatal("edit did not load saved API")
	}
	m.wiz.piAPI = "google-generative-ai"
	next, _ = m.submitForm()
	m = next.(model)
	b, found, err = st.BindingByName("pi", "work")
	if err != nil || !found || b.PiAPI != "google-generative-ai" {
		t.Fatalf("edit protocol = %q, err=%v", b.PiAPI, err)
	}
	m.clearStatus()
	if !strings.Contains(m.View(), "Gemini (Generate Content)") || strings.Contains(m.profileDetail("work"), "Anthropic") {
		t.Fatal("binding list did not refresh the saved endpoint type")
	}
	path := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "models.json")
	before, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(before), `"api": "google-generative-ai"`) {
		t.Fatalf("active edit was not applied: %v", err)
	}
	next, _ = m.startBackup("work")
	m = next.(model)
	cloned, found, err := st.BindingByName("pi", "work-copy")
	if err != nil || !found || cloned.PiAPI != b.PiAPI {
		t.Fatalf("clone protocol = %q, err=%v", cloned.PiAPI, err)
	}
	for _, dst := range []string{"pi", "claude"} {
		if err := m.copyBindingToTool("work", dst); err != nil {
			t.Fatal(err)
		}
		name, wantAPI := "work-copy-2", b.PiAPI
		if dst != "pi" {
			name, wantAPI = "work-copy", ""
		}
		cloned, found, err = st.BindingByName(dst, name)
		if err != nil || !found || cloned.PiAPI != wantAPI {
			t.Fatalf("copy to %s protocol = %q, err=%v", dst, cloned.PiAPI, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("copy changed live Pi config")
	}
}

func TestPiAPILabelDefaultAndUnknown(t *testing.T) {
	for _, tc := range []struct{ api, want string }{
		{"", "OpenAI (Chat Completions)"},
		{"unknown-api", "unknown-api"},
	} {
		if got := piAPILabel(tc.api); got != tc.want {
			t.Errorf("piAPILabel(%q) = %q, want %q", tc.api, got, tc.want)
		}
	}
}
