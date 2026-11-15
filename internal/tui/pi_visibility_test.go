package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"charon/internal/edit"
)

func TestPiVisibilityViaHKeys(t *testing.T) {
	m, id := hiddenFixture(t)
	b, found, e := m.cat.BindingByName("pi", "b")
	if e != nil || !found {
		t.Fatal(e)
	}
	cr, e := m.cat.Credential(b.CredentialID)
	if e != nil {
		t.Fatal(e)
	}
	cr2, e := m.cat.PutCredential(cr.ProviderID, "different-key")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.cat.AddBinding("pi", "b-alias", cr2.ID, "two", []string{"two"}); e != nil {
		t.Fatal(e)
	}
	a, found, e := m.cat.BindingByName("pi", "a")
	if e != nil || !found {
		t.Fatal(e)
	}
	ac, e := m.cat.Credential(a.CredentialID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.cat.PutModel(ac.ProviderID, "big"); e != nil {
		t.Fatal(e)
	}
	if _, e := m.cat.UpdateBinding(a.ID, a.Name, a.CredentialID, "one", []string{"one", "big"}); e != nil {
		t.Fatal(e)
	}
	if _, e := m.cat.Reapply(a.ID); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "settings.json")
	models := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "models.json")
	if e := edit.SetJSON(path, edit.KV{Path: "defaultModel", Value: "big"}); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	// Fixture starts hidden: show -> hide -> show, always preserving the live big.
	for _, hidden := range []bool{false, true, false} {
		m.loadProfiles("b")
		selectBinding(t, &m, "b")
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
		m = next.(model)
		if m.statusLvl == statusErr {
			t.Fatalf("h failed: %s", m.status)
		}
		for _, name := range []string{"b", "b-alias"} {
			row, ok := rowOf(m, name)
			if !ok || row.hidden != hidden {
				t.Fatalf("%s hidden=%v want=%v", name, row.hidden, hidden)
			}
			if strings.Contains(m.View(), "~ "+name) != hidden {
				t.Fatalf("bad group marker for %s:\n%s", name, m.View())
			}
		}
		if _, registered := edit.GetJSONRaw(models, "providers.charon-two-example"); registered == hidden {
			t.Fatalf("hidden=%v registered=%v", hidden, registered)
		}
		after, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(before, after) {
			t.Fatal("h reset current model/settings")
		}
		if active, ok, e := m.cat.Active("pi"); e != nil || !ok || active.ID != a.ID {
			t.Fatal("h switched binding")
		}
	}
	// Disk diverges from catalog active; h must refuse, with TUI r instructions.
	if e := edit.SetJSON(path, edit.KV{Path: "defaultProvider", Value: "charon-two-example"}); e != nil {
		t.Fatal(e)
	}
	m.loadProfiles("b")
	selectBinding(t, &m, "b")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = next.(model)
	if m.statusLvl != statusErr || !strings.Contains(m.status, "default") || !strings.Contains(m.status, "r") {
		t.Fatalf("disk-default refusal/guidance=%q", m.status)
	}
	got, found, e := m.cat.BindingByName("pi", "b")
	if e != nil || !found || got.Hidden || got.ID != id {
		t.Fatal("refusal changed saved visibility")
	}
	// Old experimental p must no longer open a policy menu.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = next.(model)
	if m.view != viewProfiles {
		t.Fatalf("p still opens experimental view %v", m.view)
	}
}
