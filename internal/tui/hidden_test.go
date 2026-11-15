package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"charon/internal/catalog"
	"charon/internal/tools"
)

// hiddenFixture builds two pi bindings, activates the first and hides the
// second, then opens the bindings screen on it.
func hiddenFixture(t *testing.T) (model, string) {
	t.Helper()
	cat := openTestCatalog(t, false)
	first, err := cat.PutProvider("https://one.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	firstKey, err := cat.PutCredential(first.ID, "sk-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.PutModel(first.ID, "one"); err != nil {
		t.Fatal(err)
	}
	second, err := cat.PutProvider("https://two.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := cat.PutCredential(second.ID, "sk-two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.PutModel(second.ID, "two"); err != nil {
		t.Fatal(err)
	}
	a, err := cat.AddBinding("pi", "a", firstKey.ID, "one", []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := cat.AddBinding("pi", "b", secondKey.ID, "two", []string{"two"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Activate(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := cat.SetBindingHidden(b.ID, true); err != nil {
		t.Fatal(err)
	}

	m := newModel(cat, "test")
	m.tool, m.view = tools.Find("pi"), viewProfiles
	m.width, m.height = 100, 30
	m.loadProfiles("")
	return m, b.ID
}

// selectBinding puts the cursor on the named binding row.
func selectBinding(t *testing.T, m *model, name string) {
	t.Helper()
	for i, raw := range m.list.Items() {
		if it, ok := raw.(item); ok && it.value == name {
			m.list.Select(i)
			return
		}
	}
	t.Fatalf("no row for binding %q", name)
}

// rowOf returns the item backing the named binding row.
func rowOf(m model, name string) (item, bool) {
	for _, raw := range m.list.Items() {
		if it, ok := raw.(item); ok && it.value == name && it.cells != nil {
			return it, true
		}
	}
	return item{}, false
}

func TestHiddenBindingRowAndToggle(t *testing.T) {
	m, hiddenID := hiddenFixture(t)
	view := m.View()
	if !strings.Contains(view, "~ b") {
		t.Fatalf("hidden binding row not marked:\n%s", view)
	}
	if !strings.Contains(view, "~ hidden") {
		t.Fatalf("bindings title does not explain the marker:\n%s", view)
	}
	// The row also carries the flag the delegate renders muted from.
	if it, ok := rowOf(m, "b"); !ok || !it.hidden {
		t.Fatalf("row b does not carry the hidden flag: %+v, ok=%v", it, ok)
	}
	if it, ok := rowOf(m, "a"); !ok || it.hidden {
		t.Fatalf("row a is marked hidden: %+v, ok=%v", it, ok)
	}

	selectBinding(t, &m, "b")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = next.(model)
	if strings.Contains(m.View(), "~ b") {
		t.Fatalf("row still marked hidden after toggle:\n%s", m.View())
	}
	if !strings.Contains(m.status, "Showed b") {
		t.Fatalf("status = %q, want a Showed hint", m.status)
	}
	for _, b := range mustBindings(t, m) {
		if b.ID == hiddenID && b.Hidden {
			t.Fatal("toggle left the binding hidden in the catalog")
		}
	}
}

func TestHidingActiveBindingIsRefused(t *testing.T) {
	m, _ := hiddenFixture(t)
	selectBinding(t, &m, "a")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = next.(model)
	if !strings.Contains(m.status, "active") {
		t.Fatalf("status = %q, want a refusal naming the active binding", m.status)
	}
	if !strings.Contains(m.View(), "~ b") {
		t.Fatalf("refusal changed the other row:\n%s", m.View())
	}
}

// TestDuplicateHiddenBindingViaKeys drives the duplicate wizard through real
// Update keypresses: c/x on the hidden b row, Enter on "This tool", edit the
// suggested name, Enter. The clone must keep b's hidden flag, credential,
// models, protocol and headers, while leaving b, the active binding and Pi's
// on-disk models/settings byte for byte untouched. A later explicit Reapply(a)
// resets the live default by design but must still keep the hidden gateway key
// out of Pi's provider list.
func TestDuplicateHiddenBindingViaKeys(t *testing.T) {
	for _, shortcut := range []rune{'c', 'x'} {
		t.Run(string(shortcut), func(t *testing.T) {
			m, _ := hiddenFixture(t)
			cat := m.cat

			b, found, err := cat.BindingByName("pi", "b")
			if err != nil || !found {
				t.Fatalf("b binding: found=%v err=%v", found, err)
			}
			b, err = cat.UpdateBinding(b.ID, b.Name, b.CredentialID, "two", []string{"two"}, "anthropic-messages")
			if err != nil {
				t.Fatalf("set b protocol: %v", err)
			}
			headers := map[string]string{"X-Backup": "hdr-backup"}
			b, err = cat.SetBindingHeaders(b.ID, headers)
			if err != nil {
				t.Fatalf("set b headers: %v", err)
			}

			a, found, err := cat.BindingByName("pi", "a")
			if err != nil || !found {
				t.Fatalf("a binding: found=%v err=%v", found, err)
			}
			cr, err := cat.Credential(a.CredentialID)
			if err != nil {
				t.Fatalf("a credential: %v", err)
			}
			provider, err := cat.Provider(cr.ProviderID)
			if err != nil {
				t.Fatalf("a provider: %v", err)
			}
			if _, err := cat.PutModel(provider.ID, "one-live"); err != nil {
				t.Fatalf("add one-live model: %v", err)
			}
			a, err = cat.UpdateBinding(a.ID, "a", a.CredentialID, "one", []string{"one", "one-live"})
			if err != nil {
				t.Fatalf("set a models: %v", err)
			}
			// Clear the group the fixture applied before b was hidden, and render a's
			// two-model list.
			if _, err := cat.Reapply(a.ID); err != nil {
				t.Fatalf("reapply a: %v", err)
			}

			// Pi's own picker selected a non-saved default; copying must not reset it.
			settingsPath := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "settings.json")
			modelsPath := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "models.json")
			data, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatalf("read settings: %v", err)
			}
			var settings map[string]any
			if err := json.Unmarshal(data, &settings); err != nil {
				t.Fatalf("decode settings: %v", err)
			}
			settings["defaultModel"] = "one-live"
			updated, err := json.Marshal(settings)
			if err != nil {
				t.Fatalf("encode settings: %v", err)
			}
			if err := os.WriteFile(settingsPath, updated, 0o600); err != nil {
				t.Fatalf("select one-live: %v", err)
			}

			m.loadProfiles("")
			bBefore, found, err := cat.BindingByName("pi", "b")
			if err != nil || !found || !bBefore.Hidden {
				t.Fatalf("b before copy: %+v found=%v err=%v", bBefore, found, err)
			}
			aBefore, found, err := cat.BindingByName("pi", "a")
			if err != nil || !found {
				t.Fatalf("a before copy: found=%v err=%v", found, err)
			}
			modelsBefore, err := os.ReadFile(modelsPath)
			if err != nil {
				t.Fatalf("read models before copy: %v", err)
			}
			settingsBefore, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatalf("read settings before copy: %v", err)
			}

			selectBinding(t, &m, "b")
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{shortcut}})
			m = next.(model)
			if m.view != viewCopyTool {
				t.Fatalf("%q on b = view %v, want viewCopyTool", shortcut, m.view)
			}
			if it, ok := m.list.SelectedItem().(item); !ok || it.value != "pi" {
				t.Fatalf("first destination row = %+v, want This tool (pi)", it)
			}
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(model)
			if m.view != viewDupName {
				t.Fatalf("Enter on This tool = view %v, want viewDupName; status=%q", m.view, m.status)
			}
			if got := m.input.Value(); got != "b-copy" {
				t.Fatalf("prefilled name = %q, want b-copy", got)
			}
			for range len(m.input.Value()) {
				next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
				m = next.(model)
			}
			const cloneName = "b-clone"
			for _, r := range cloneName {
				next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				m = next.(model)
			}
			if got := m.input.Value(); got != cloneName {
				t.Fatalf("typed name = %q, want %q", got, cloneName)
			}
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(model)
			if m.view != viewProfiles {
				t.Fatalf("name submit = view %v, want viewProfiles; status=%q", m.view, m.status)
			}
			if !strings.Contains(m.View(), "~ "+cloneName) {
				t.Fatalf("clone row is not marked hidden:\n%s", m.View())
			}
			if it, ok := m.list.SelectedItem().(item); !ok || it.value != cloneName {
				t.Fatalf("cursor after clone = %+v, want %q", it, cloneName)
			}
			if row, ok := rowOf(m, cloneName); !ok || !row.hidden {
				t.Fatalf("clone row hidden flag = %+v, ok=%v", row, ok)
			}

			clone, found, err := cat.BindingByName("pi", cloneName)
			if err != nil || !found {
				t.Fatalf("clone missing: found=%v err=%v", found, err)
			}
			if !clone.Hidden {
				t.Errorf("clone Hidden = false, want the source's hidden flag")
			}
			if clone.CredentialID != bBefore.CredentialID {
				t.Errorf("clone credential = %q, want shared %q", clone.CredentialID, bBefore.CredentialID)
			}
			if clone.ModelID != bBefore.ModelID || !reflect.DeepEqual(clone.Models, bBefore.Models) {
				t.Errorf("clone models = %q %v, want %q %v", clone.ModelID, clone.Models, bBefore.ModelID, bBefore.Models)
			}
			if clone.PiAPI != bBefore.PiAPI {
				t.Errorf("clone protocol = %q, want %q", clone.PiAPI, bBefore.PiAPI)
			}
			if !reflect.DeepEqual(clone.Headers, bBefore.Headers) {
				t.Errorf("clone headers = %#v, want %#v", clone.Headers, bBefore.Headers)
			}
			cloneKey, err := cat.Credential(clone.CredentialID)
			if err != nil || cloneKey.Key != "sk-two" {
				t.Errorf("clone key = %q err=%v, want sk-two", cloneKey.Key, err)
			}

			bAfter, found, err := cat.BindingByName("pi", "b")
			if err != nil || !found || !reflect.DeepEqual(bAfter, bBefore) {
				t.Errorf("source b changed: before=%+v after=%+v err=%v", bBefore, bAfter, err)
			}
			aAfter, found, err := cat.BindingByName("pi", "a")
			if err != nil || !found || !reflect.DeepEqual(aAfter, aBefore) {
				t.Errorf("binding a changed: before=%+v after=%+v err=%v", aBefore, aAfter, err)
			}
			if active, found, err := cat.Active("pi"); err != nil || !found || active.Name != "a" {
				t.Errorf("active after copy = %+v found=%v err=%v, want a", active, found, err)
			}

			modelsAfter, err := os.ReadFile(modelsPath)
			if err != nil || string(modelsAfter) != string(modelsBefore) {
				t.Errorf("copy rewrote Pi models.json (err=%v)", err)
			}
			settingsAfter, err := os.ReadFile(settingsPath)
			if err != nil || string(settingsAfter) != string(settingsBefore) {
				t.Errorf("copy rewrote Pi settings.json (err=%v)", err)
			}
			var disk struct {
				DefaultModel string `json:"defaultModel"`
			}
			if err := json.Unmarshal(settingsAfter, &disk); err != nil || disk.DefaultModel != "one-live" {
				t.Errorf("Pi defaultModel after copy = %q err=%v, want one-live", disk.DefaultModel, err)
			}

			// Explicit reapply is expected to reset the live default to a's saved
			// default. It must not, however, register the hidden gateway or its copy.
			if _, err := cat.Reapply(a.ID); err != nil {
				t.Fatalf("reapply a after copy: %v", err)
			}
			reapplied, err := os.ReadFile(modelsPath)
			if err != nil {
				t.Fatalf("read models after reapply: %v", err)
			}
			for _, leaked := range []string{"sk-two", "https://two.example/v1"} {
				if strings.Contains(string(reapplied), leaked) {
					t.Errorf("reapply registered hidden gateway %q", leaked)
				}
			}
			var reappliedSettings struct {
				DefaultModel string `json:"defaultModel"`
			}
			settingsReapplied, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatalf("read settings after reapply: %v", err)
			}
			if err := json.Unmarshal(settingsReapplied, &reappliedSettings); err != nil || reappliedSettings.DefaultModel != "one" {
				t.Errorf("explicit reapply defaultModel = %q err=%v, want one (reapply resets by design)", reappliedSettings.DefaultModel, err)
			}
			for _, name := range []string{"b", cloneName} {
				got, found, err := cat.BindingByName("pi", name)
				if err != nil || !found || !got.Hidden {
					t.Errorf("%s after reapply: Hidden=%v found=%v err=%v, want hidden", name, got.Hidden, found, err)
				}
			}
			if active, found, err := cat.Active("pi"); err != nil || !found || active.Name != "a" {
				t.Errorf("active after reapply = %+v found=%v err=%v, want a", active, found, err)
			}
		})
	}
}

func mustBindings(t *testing.T, m model) []catalog.Binding {
	t.Helper()
	bs, err := m.cat.Bindings(m.tool.Name)
	if err != nil {
		t.Fatal(err)
	}
	return bs
}
