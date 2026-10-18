package tui

import (
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

func mustBindings(t *testing.T, m model) []catalog.Binding {
	t.Helper()
	bs, err := m.cat.Bindings(m.tool.Name)
	if err != nil {
		t.Fatal(err)
	}
	return bs
}
