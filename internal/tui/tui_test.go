package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"charon/internal/catalog"
	"charon/internal/models"
	"charon/internal/tools"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func openTestCatalog(t *testing.T, detectCodex bool) *catalog.Catalog {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "tester")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", t.TempDir()) // keeps Claude detection from querying the real Keychain
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_API_KEY":"sk-test"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if detectCodex {
		path := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c, err := catalog.Open()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// errFetchFailed stands in for a models-endpoint failure (404, auth, no such route).
var errFetchFailed = errors.New("API returned 404 Not Found (check endpoint and key)")

func TestStatusRender(t *testing.T) {
	tests := []struct {
		name       string
		level      statusLevel
		msg        string
		wantEmpty  bool
		wantSubstr string // substring that must appear in the rendered line
	}{
		{name: "empty message renders nothing", level: statusOK, msg: "", wantEmpty: true},
		{name: "info has no glyph", level: statusInfo, msg: "cancelled", wantSubstr: "cancelled"},
		{name: "ok gets a check", level: statusOK, msg: "Switched", wantSubstr: "✓"},
		{name: "err gets a cross", level: statusErr, msg: "boom", wantSubstr: "✗"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := statusRender(tt.level, tt.msg)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("statusRender(%v, %q) = %q, want empty", tt.level, tt.msg, got)
				}
				return
			}
			if !strings.Contains(got, tt.wantSubstr) {
				t.Fatalf("statusRender(%v, %q) = %q, want substring %q", tt.level, tt.msg, got, tt.wantSubstr)
			}
			if !strings.Contains(got, tt.msg) {
				t.Fatalf("statusRender(%v, %q) = %q, want it to contain the message", tt.level, tt.msg, got)
			}
		})
	}
}

func TestConfigSummaryDoesNotInferDefaultsOrExposeSecrets(t *testing.T) {
	info := tools.Info{Model: "gpt-6-luna", Secret: "sk-do-not-show"}
	summary := configSummary(info)
	for _, want := range []string{"On-disk config", "gpt-6-luna", "Context window: unknown", "Max output (maxTokens): unknown", "Reasoning effort: unknown", "running sessions may differ"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q: %s", want, summary)
		}
	}
	if strings.Contains(summary, info.Secret) || strings.Contains(summary, "128,000") || strings.Contains(summary, "600,000") {
		t.Fatal("summary exposed a secret or invented limits from presets")
	}
}

// piDefaultProvider is the provider key charon made default in settings.json:
// the endpoint-keyed name of the active binding's own entry.
func piDefaultProvider(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".pi", "agent", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		DefaultProvider string `json:"defaultProvider"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.DefaultProvider == "" {
		t.Fatal("settings.json has no defaultProvider")
	}
	return s.DefaultProvider
}

func TestBindingConfigSummaryRefreshIsReadOnly(t *testing.T) {
	st := openTestCatalog(t, false)
	tool := tools.Find("pi")
	if err := tool.ApplyAuth(tools.AuthSpec{
		Endpoint: "https://example.test/v1", Key: "sk-test", Model: "gpt-6-luna",
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "models.json")
	// Deliberately differ from current presets, like an old installed binary.
	const oldConfig = `{"providers":{"charon":{"apiKey":"sk-do-not-show","models":[{"id":"gpt-6-luna","contextWindow":500000,"maxTokens":8192}]}}}`
	if err := os.WriteFile(path, []byte(oldConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newModel(st, "test")
	m.tool, m.view = tool, viewProfiles
	m.width, m.height = 80, 30
	m.loadProfiles("")
	for _, size := range [][2]int{{48, 24}, {80, 24}, {120, 30}} {
		m.width, m.height = size[0], size[1]
		width := m.width
		m.resize()
		view := m.View()
		for _, want := range []string{"On-disk config", "500,000", "8,192"} {
			if !strings.Contains(view, want) {
				t.Errorf("width %d: view missing %q", width, want)
			}
		}
		if strings.Contains(view, "sk-do-not-show") {
			t.Fatal("view exposed the raw API key")
		}
		if rows := len(strings.Split(view, "\n")); rows != m.height {
			t.Fatalf("width %d: view has %d rows, want %d", width, rows, m.height)
		}
	}
	const changed = `{"providers":{"charon":{"apiKey":"sk-do-not-show","models":[{"id":"gpt-6-luna","contextWindow":420000,"maxTokens":32000}]}}}`
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	refreshed := next.(model)
	if view := refreshed.View(); !strings.Contains(view, "420,000") || !strings.Contains(view, "32,000") || strings.Contains(view, "8,192") {
		t.Fatalf("refresh did not read updated on-disk limits:\n%s", view)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != changed {
		t.Fatal("refresh rewrote the model config instead of reading it")
	}
}

func TestThemedDelegateUsesTightSpacing(t *testing.T) {
	if got := themedDelegate().Spacing(); got != 1 {
		t.Fatalf("themedDelegate spacing = %d, want 1", got)
	}
}

func TestWizardStep(t *testing.T) {
	tests := []struct {
		view      view
		wantN     int
		wantTotal int
		wantLabel string
	}{
		{viewAddEndpoint, 1, 4, "endpoint"},
		{viewAddKey, 2, 4, "API key"},
		{viewFetching, 3, 4, "choose a model"},
		{viewPickModel, 3, 4, "choose a model"},
		{viewAddName, 4, 4, "name the binding"},
		// Non-wizard views report no progress.
		{viewTools, 0, 0, ""},
		{viewProfiles, 0, 0, ""},
		{viewEditForm, 0, 0, ""},
		{viewEditField, 0, 0, ""},
		{viewEditField, 0, 0, ""},
	}
	for _, tt := range tests {
		n, total, label := wizardStep(tt.view)
		if n != tt.wantN || total != tt.wantTotal || label != tt.wantLabel {
			t.Errorf("wizardStep(%v) = (%d, %d, %q), want (%d, %d, %q)",
				tt.view, n, total, label, tt.wantN, tt.wantTotal, tt.wantLabel)
		}
	}
}

func TestEditFormAlwaysShowsNameField(t *testing.T) {
	m := model{tool: &tools.Tool{Title: "Fake"}, wiz: wizard{name: "work", origName: "work", edit: true}}
	m.loadEditForm()
	if m.formInputs[focusName].Value() != "work" {
		t.Fatalf("name field = %q, want work", m.formInputs[focusName].Value())
	}
}

func TestFilterModels(t *testing.T) {
	all := []string{"gpt-4o", "gpt-4o-mini", "claude-opus-4-8", "claude-sonnet-5", "o3-mini"}

	// An empty (or whitespace-only) query returns the full list unchanged.
	if got := filterModels(all, ""); len(got) != len(all) {
		t.Fatalf("empty query returned %d items, want %d", len(got), len(all))
	}
	if got := filterModels(all, "   "); len(got) != len(all) {
		t.Fatalf("whitespace query returned %d items, want %d", len(got), len(all))
	}

	// A query narrows to fuzzy matches only.
	got := filterModels(all, "claude")
	if len(got) != 2 {
		t.Fatalf("filterModels(claude) = %v, want 2 matches", got)
	}
	for _, id := range got {
		if !strings.Contains(id, "claude") {
			t.Fatalf("filterModels(claude) returned non-match %q", id)
		}
	}

	// Fuzzy (non-contiguous) matching works and ranks the closer id first.
	if got := filterModels(all, "gpt4o"); len(got) == 0 || got[0] != "gpt-4o" {
		t.Fatalf("filterModels(gpt4o) = %v, want best match gpt-4o", got)
	}

	// A query that matches nothing yields an empty result.
	if got := filterModels(all, "zzzz"); len(got) != 0 {
		t.Fatalf("filterModels(zzzz) = %v, want no matches", got)
	}
}

// TestWizardStepsAreSequential guards that the add-flow steps are numbered
// 1..total with a consistent total, so the progress line never lies.
func TestWizardStepsAreSequential(t *testing.T) {
	flow := []view{viewAddEndpoint, viewAddKey, viewPickModel, viewAddName}
	for i, v := range flow {
		n, total, _ := wizardStep(v)
		if total != len(flow) {
			t.Errorf("view %v: total = %d, want %d", v, total, len(flow))
		}
		if n != i+1 {
			t.Errorf("view %v: step = %d, want %d", v, n, i+1)
		}
	}
}

func TestSkipSeparators(t *testing.T) {
	l := list.New([]list.Item{
		item{title: "p1", value: "p1"},
		item{value: sepSentinel},
		item{title: "＋ Add", value: addSentinel},
	}, themedDelegate(), 40, 20)
	m := &model{list: l, view: viewProfiles}

	// Moving down onto the divider (idx 1) should continue to the action row (idx 2).
	m.list.Select(0)
	before := m.list.Index()
	m.list.CursorDown()
	m.skipSeparators(before)
	if got := m.list.Index(); got != 2 {
		t.Errorf("down: index = %d, want 2 (divider skipped)", got)
	}

	// Moving up onto the divider should continue back to the binding (idx 0).
	before = m.list.Index()
	m.list.CursorUp()
	m.skipSeparators(before)
	if got := m.list.Index(); got != 0 {
		t.Errorf("up: index = %d, want 0 (divider skipped)", got)
	}

	// The table's column labels and drift annotations are inert the same way.
	l2 := list.New([]list.Item{
		item{title: "saved: x · live: y", value: driftSentinel + "p1"},
		item{title: "p1", value: "p1"},
		item{cells: bindingTableHeader, value: hdrSentinel},
		item{title: "p2", value: "p2"},
	}, themedTableDelegate(), 40, 20)
	m2 := &model{list: l2, view: viewProfiles}
	m2.list.Select(1)
	before = m2.list.Index()
	m2.list.CursorDown() // onto the column labels (idx 2)
	m2.skipSeparators(before)
	if got := m2.list.Index(); got != 3 {
		t.Errorf("down onto labels: index = %d, want 3 (labels skipped)", got)
	}
	before = m2.list.Index()
	m2.list.CursorUp() // back onto the column labels
	m2.skipSeparators(before)
	if got := m2.list.Index(); got != 1 {
		t.Errorf("up onto labels: index = %d, want 1 (labels skipped)", got)
	}
}

// TestModelsTableSkipsLabelsAndAlignsColumns covers the model-library screen:
// the cursor opens on the first data row under the column labels, moving up
// skips the labels onto the add row, and every row shares the header's
// columns.
func TestModelsTableSkipsLabelsAndAlignsColumns(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://gateway.example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "glm-4.7"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "kimi-k2"); err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.view = viewModels
	m.width, m.height = 100, 24
	m.resize()
	m.loadModels("")

	if it, ok := m.list.SelectedItem().(item); !ok || it.title != "glm-4.7" {
		t.Fatalf("cursor opened on %+v, want the first model row", it)
	}
	before := m.list.Index()
	m.list.CursorUp() // onto the column labels
	m.skipSeparators(before)
	if it, ok := m.list.SelectedItem().(item); !ok || it.value != addSentinel {
		t.Fatalf("cursor above the first row = %+v, want the add row", it)
	}

	lines := renderTableLines(&m)
	if len(lines) != 4 {
		t.Fatalf("rendered %d rows, want 4 (labels, two models, add): %q", len(lines), lines)
	}
	header := lines[0]
	endpointCol := displayColumn(header, strings.Index(header, "endpoint"))
	contextCol := displayColumn(header, strings.Index(header, "context"))
	for i := 1; i <= 2; i++ {
		if got := displayColumn(lines[i], strings.Index(lines[i], "https://gateway.example.com/v1")); got != endpointCol {
			t.Errorf("row %d endpoint starts at column %d, want %d: %q", i, got, endpointCol, lines[i])
		}
	}
	if got := displayColumn(lines[1], strings.Index(lines[1], "204,800")); got != contextCol {
		t.Errorf("row 1 context starts at column %d, want %d: %q", got, contextCol, lines[1])
	}
}

// seedModelLibrary saves three models across two endpoints; glm-4.7 exists on
// both so a search has duplicates to resolve.
func seedModelLibrary(t *testing.T) *catalog.Catalog {
	t.Helper()
	st := openTestCatalog(t, false)
	p1, err := st.PutProvider("https://one.example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := st.PutProvider("https://two.example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"glm-4.7", "gpt-5"} {
		if _, err := st.PutModel(p1.ID, slug); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.PutModel(p2.ID, "glm-4.7"); err != nil {
		t.Fatal(err)
	}
	return st
}

func modelLibraryTitles(m *model) []string {
	var out []string
	for _, raw := range m.list.Items() {
		if it, ok := raw.(item); ok && !isSentinel(it.value) {
			out = append(out, it.title)
		}
	}
	return out
}

func typeQuery(t *testing.T, m *model, q string) {
	t.Helper()
	for _, r := range q {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = next.(model)
	}
}

func TestModelLibrarySearchFiltersBySlugAndEndpoint(t *testing.T) {
	st := seedModelLibrary(t)
	m := newModel(st, "test")
	m.view = viewModels
	m.width, m.height = 100, 24
	m.resize()
	m.loadModels("")

	// Endpoint query narrows to one gateway's models.
	typeQuery(t, &m, "one.example")
	if got := modelLibraryTitles(&m); len(got) != 2 || got[0] != "glm-4.7" || got[1] != "gpt-5" {
		t.Errorf("rows after endpoint search = %v, want one.example's two models", got)
	}
	if !strings.Contains(m.list.Title, "search: one.example (2 of 3)") {
		t.Errorf("title = %q, want the search summary with the match count", m.list.Title)
	}
	if it, ok := m.list.SelectedItem().(item); !ok || isSentinel(it.value) {
		t.Errorf("cursor = %+v while searching, want the first data row", it)
	}

	// Backspace trims the query; the rows widen back as it shortens.
	for range 2 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = next.(model)
	}
	if m.modelFilter != "one.examp" {
		t.Errorf("filter after two backspaces = %q, want one.examp", m.modelFilter)
	}
	if got := modelLibraryTitles(&m); len(got) != 2 {
		t.Errorf("rows after trimming = %v, want the same two", got)
	}

	// Slug query spans endpoints: glm-4.7 is saved on both.
	m.modelFilter = ""
	m.loadModels("")
	typeQuery(t, &m, "glm")
	if got := modelLibraryTitles(&m); len(got) != 2 {
		t.Errorf("rows after slug search = %v, want the two glm-4.7 rows", got)
	}

	// A query matching nothing leaves an inert notice row, not an empty list.
	m.modelFilter = "zzz-nothing"
	m.loadModels("")
	if got := modelLibraryTitles(&m); len(got) != 0 {
		t.Errorf("rows for a miss = %v, want none", got)
	}
	sawNotice := false
	for _, raw := range m.list.Items() {
		if it, ok := raw.(item); ok && it.value == noneSentinel && strings.Contains(it.title, "No match") {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Error("a miss rendered no notice row")
	}

	// esc clears the search and restores every row; the footer swaps its hint.
	if !strings.Contains(m.View(), "clear filter") {
		t.Errorf("footer while searching = %q, want the clear-filter hint", m.View())
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.modelFilter != "" {
		t.Errorf("esc left the search at %q", m.modelFilter)
	}
	if got := modelLibraryTitles(&m); len(got) != 3 {
		t.Errorf("rows after clearing = %v, want all three", got)
	}
	if !strings.Contains(m.View(), "esc back") || strings.Contains(m.View(), "clear filter") {
		t.Error("footer did not return to the plain back hint after clearing")
	}
}

func TestModelLibrarySearchLettersBelongToQuery(t *testing.T) {
	st := seedModelLibrary(t)
	m := newModel(st, "test")
	m.view = viewModels
	m.width, m.height = 100, 24
	m.resize()
	m.loadModels("")

	// Once a search is underway, the screen's own shortcut keys type into it
	// instead of firing — otherwise a query containing e/w/d/r/q is untypable.
	typeQuery(t, &m, "gpt")
	for _, r := range "edwrq" {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(model)
	}
	if m.modelFilter != "gptedwrq" {
		t.Errorf("search = %q, want every shortcut letter to land in the query", m.modelFilter)
	}
	if m.view != viewModels {
		t.Errorf("view = %v, want to stay on the library", m.view)
	}

	// Before any search, the shortcuts still command: d deletes the row.
	stale := seedModelLibrary(t)
	m2 := newModel(stale, "test")
	m2.view = viewModels
	m2.width, m2.height = 100, 24
	m2.resize()
	m2.loadModels("")
	next, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m2 = next.(model)
	if m2.view != viewModels {
		t.Fatalf("view = %v after d, want the library", m2.view)
	}
	models, err := stale.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Errorf("catalog holds %d models after d, want 2 (one deleted)", len(models))
	}
}

// TestModelLibrarySearchResetsOnEntry pins that a fresh visit starts
// unfiltered — the query is transient, like the picker's.
func TestModelLibrarySearchResetsOnEntry(t *testing.T) {
	st := seedModelLibrary(t)
	m := newModel(st, "test")
	m.view = viewTools
	m.width, m.height = 100, 24
	m.resize()
	m.loadTools()
	m.modelFilter = "stale"
	m.list.Select(indexOfLibraryRow(&m))

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.view != viewModels {
		t.Fatalf("view = %v, want the model library", m.view)
	}
	if m.modelFilter != "" {
		t.Errorf("entered the library with a stale search %q", m.modelFilter)
	}
	if got := modelLibraryTitles(&m); len(got) != 3 {
		t.Errorf("rows = %v, want all three models", got)
	}
}

func indexOfLibraryRow(m *model) int {
	for i, raw := range m.list.Items() {
		if it, ok := raw.(item); ok && it.value == libSentinel {
			return i
		}
	}
	return 0
}

// TestBindingsTableAlignsColumns renders the bindings screen through the table
// delegate and checks that every row lays its cells out on the same columns as
// the header labels — the whole point of the table form — and that each row is
// exactly one line, which is what keeps pagination honest.
func TestBindingsTableAlignsColumns(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://gateway.example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := st.PutCredential(p.ID, "sk-gateway-123456789")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "glm-4.7"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "kimi-k2"); err != nil {
		t.Fatal(err)
	}
	work, err := st.AddBinding("claude", "work", cr.ID, "glm-4.7", []string{"glm-4.7", "kimi-k2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBinding("claude", "backup", cr.ID, "kimi-k2", []string{"kimi-k2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Activate(work.ID); err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.tool, m.view = tools.Find("claude"), viewProfiles
	m.width, m.height = 100, 30
	m.loadProfiles("work")

	lines := renderTableLines(&m)
	if len(lines) != 5 {
		t.Fatalf("rendered %d rows, want 5 (add, divider, header, two bindings): %q", len(lines), lines)
	}
	for i, line := range lines {
		if strings.Contains(line, "\n") {
			t.Fatalf("row %d is not a single line: %q", i, line)
		}
	}
	// Bindings render name-sorted: backup first, then the active work binding.
	header := lines[2]
	endpointCol := displayColumn(header, strings.Index(header, "endpoint"))
	modelCol := displayColumn(header, strings.Index(header, "model"))
	if endpointCol < 0 || modelCol <= endpointCol {
		t.Fatalf("header row = %q, want endpoint left of model", header)
	}
	for _, i := range []int{3, 4} {
		at := strings.Index(lines[i], "https://gateway.example.com/v1")
		if got := displayColumn(lines[i], at); got != endpointCol {
			t.Errorf("row %d endpoint starts at column %d, want %d: %q", i, got, endpointCol, lines[i])
		}
	}
	if got := displayColumn(lines[3], strings.Index(lines[3], "kimi-k2")); got != modelCol {
		t.Errorf("backup row model starts at column %d, want %d: %q", got, modelCol, lines[3])
	}
	if got := displayColumn(lines[4], strings.Index(lines[4], "glm-4.7")); got != modelCol {
		t.Errorf("work row model starts at column %d, want %d: %q", got, modelCol, lines[4])
	}
	if !strings.Contains(lines[4], "+1") {
		t.Errorf("work row should count its extra picker model: %q", lines[4])
	}
}

// displayColumn converts a byte offset in s to its display column. Every
// character in a table row is one column wide (the ✓ marker included), so the
// rune count is the column.
func displayColumn(s string, byteOffset int) int {
	if byteOffset < 0 || byteOffset > len(s) {
		return -1
	}
	return utf8.RuneCountInString(s[:byteOffset])
}

// renderTableLines renders every item of the model's list through the table
// delegate, one string per row.
func renderTableLines(m *model) []string {
	d := themedTableDelegate()
	var lines []string
	for i, raw := range m.list.Items() {
		var buf strings.Builder
		d.Render(&buf, m.list, i, raw)
		lines = append(lines, buf.String())
	}
	return lines
}

func TestQuitKeyDisabledInPicker(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "v1.0.0")
	if m.list.KeyMap.Quit.Enabled() {
		t.Error("Quit key should be disabled after newModel")
	}

	m.tool = m.allTools[0]
	m.loadProfiles("")
	if m.list.KeyMap.Quit.Enabled() {
		t.Error("Quit key should be disabled after loadProfiles")
	}

	m.view = viewPickModel
	m.showModels([]string{"gpt-4o", "claude-3-5-sonnet"})
	if m.list.KeyMap.Quit.Enabled() {
		t.Error("Quit key should be disabled after showModels")
	}

	helpView := m.list.Help.View(m.list)
	if strings.Contains(helpView, "q quit") {
		t.Errorf("ShortHelp view contains 'q quit': %q", helpView)
	}
}

func TestIsSentinel(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{addSentinel, true},
		{sepSentinel, true},
		{"work", false},
		{"default", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run("sentinel_"+tt.value, func(t *testing.T) {
			if got := isSentinel(tt.value); got != tt.want {
				t.Errorf("isSentinel(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestModelLibraryIsLocalEntryAndEditor(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "test")
	m.tool = tools.Find("codex")
	m.loadTools()

	if got := len(m.list.Items()); got == 0 || m.list.Items()[got-1].(item).value != libSentinel {
		t.Fatalf("tools list does not end with Model Library entry: %+v", m.list.Items())
	}

	m.list.Select(len(m.list.Items()) - 1)
	if next, _ := m.onEnter(); true {
		m = next.(model)
	}
	if m.view != viewModels {
		t.Fatalf("view = %v, want viewModels", m.view)
	}
	if strings.Contains(m.list.Title, "local") == false {
		t.Fatalf("model library title = %q, want local", m.list.Title)
	}

	m.view = viewModelEndpoint
	m.modelEditEndpoint = "https://gateway.example/v1"
	m.startInput("endpoint", false)
	m.input.SetValue("https://gateway.example/v1")
	if next, _ := m.updateInput(tea.KeyMsg{Type: tea.KeyEnter}); true {
		m = next.(model)
	}
	if m.view != viewModelSlug {
		t.Fatalf("view after endpoint = %v, want viewModelSlug", m.view)
	}
	m.input.SetValue("glm-4.7")
	if next, _ := m.updateInput(tea.KeyMsg{Type: tea.KeyEnter}); true {
		m = next.(model)
	}
	if m.view != viewWindowPresets {
		t.Fatalf("view after slug = %v, want viewWindowPresets", m.view)
	}
	if it, ok := m.list.SelectedItem().(item); !ok || it.value != "0" {
		t.Fatalf("preset cursor = %+v, want the Unknown row for a new model", it)
	}
	m.list.Select(indexOfValue(m.list.Items(), "200000"))
	if next, _ := m.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEnter}); true {
		m = next.(model)
	}
	if m.view != viewModels {
		t.Fatalf("view after window = %v, want viewModels", m.view)
	}

	models, err := st.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Slug != "glm-4.7" || models[0].ContextWindow != 200000 || models[0].ContextWindowSource != catalog.WindowManual {
		t.Fatalf("saved local model = %+v", models)
	}
	rows := m.list.Items()
	if len(rows) < 2 || rows[0].(item).value != hdrSentinel {
		t.Fatalf("model library rows = %+v, want the column labels first", rows)
	}
	data := rows[1].(item)
	if data.title != "glm-4.7" || len(data.cells) != 4 {
		t.Fatalf("model library row = %+v", data)
	}
	if !strings.Contains(m.list.Title, "local") {
		t.Fatalf("model library title = %q", m.list.Title)
	}
	joined := strings.Join(data.cells, " ")
	if !strings.Contains(joined, "200,000") || !strings.Contains(joined, "manual") {
		t.Fatalf("model library cells = %q, want window and source", data.cells)
	}
}

func TestModelLibraryEditsEffort(t *testing.T) {
	st := openTestCatalog(t, true)
	p, err := st.PutProvider("https://gateway.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	m1, err := st.PutModel(p.ID, "glm-4.7")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := st.PutCredential(p.ID, "sk-gateway-123456789")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.AddBinding("codex", "gw", cr.ID, "glm-4.7", []string{"glm-4.7"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Activate(b.ID); err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.loadTools()
	m.view = viewModels
	m.loadModels(m1.ID)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	inEdit := next.(model)
	if inEdit.view != viewModelEffort {
		t.Fatalf("view = %v, want viewModelEffort", inEdit.view)
	}
	inEdit.input.SetValue("HIGH")
	saved, _ := inEdit.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	if got := saved.(model); got.view != viewModels {
		t.Fatalf("view after saving effort = %v, want viewModels", got.view)
	}
	stored, err := st.Model(m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Effort != "high" {
		t.Fatalf("saved effort = %q, want high", stored.Effort)
	}
	cfg := filepath.Join(t.TempDir(), "missing")
	if home := os.Getenv("HOME"); home != "" {
		cfg = filepath.Join(home, ".codex", "config.toml")
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "model_reasoning_effort = \"high\"") {
		t.Fatalf("Codex config does not carry effort:\n%s", data)
	}
}

func TestModelLibraryRefreshesActivePiThinkingMap(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://gateway.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := st.PutModel(p.ID, "custom")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "other"); err != nil {
		t.Fatal(err)
	}
	cr, err := st.PutCredential(p.ID, "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	active, err := st.AddBinding("pi", "active", cr.ID, "custom", []string{"custom", "other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Activate(active.ID); err != nil {
		t.Fatal(err)
	}
	// An inactive binding referencing the same model must not replace the
	// active binding's default model or shrink its picker during the refresh.
	if _, err := st.AddBinding("pi", "inactive", cr.ID, "other", []string{"custom", "other"}); err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	claudePath := filepath.Join(home, ".claude", "settings.json")
	claudeBefore, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.editingModel = stored
	m.view = viewModelEffort
	m.input.SetValue("high=maximum,off=off")
	next, _ := m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(model); got.view != viewModels {
		t.Fatalf("view after saving = %v, want viewModels", got.view)
	}
	data, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Providers map[string]struct {
			Models []struct {
				ID               string             `json:"id"`
				Reasoning        bool               `json:"reasoning"`
				ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	entries := config.Providers[piDefaultProvider(t)].Models
	if len(entries) != 2 || entries[0].ID != "custom" {
		t.Fatalf("active picker changed: %+v", entries)
	}
	high := entries[0].ThinkingLevelMap["high"]
	off, hasOff := entries[0].ThinkingLevelMap["off"]
	if !entries[0].Reasoning || high == nil || *high != "maximum" || !hasOff || off != nil {
		t.Fatalf("Pi thinking map not refreshed: %+v", entries[0])
	}
	info, err := tools.Find("pi").Describe()
	if err != nil || info.Model != "custom" {
		t.Fatalf("active Pi default replaced: %+v, %v", info, err)
	}
	claudeAfter, err := os.ReadFile(claudePath)
	if err != nil || string(claudeBefore) != string(claudeAfter) {
		t.Fatal("unrelated tool configuration changed")
	}
}

func TestInputView(t *testing.T) {
	m := model{}
	for _, v := range []view{viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField} {
		m.view = v
		if !m.inputView() {
			t.Errorf("inputView() = false for view %v, want true", v)
		}
	}
	// Non-input views report false.
	for _, v := range []view{viewTools, viewProfiles, viewFetching, viewPickModel} {
		m.view = v
		if m.inputView() {
			t.Errorf("inputView() = true for view %v, want false", v)
		}
	}
	// EditForm with a focused field is an input view.
	m.view = viewEditForm
	m.formFocus = 0
	if !m.inputView() {
		t.Error("inputView() = false for viewEditForm with formFocus=0, want true")
	}
	m.formFocus = focusModels // action row, not an input field
	if m.inputView() {
		t.Error("inputView() = true for viewEditForm with formFocus=4, want false")
	}
}

func TestFindTool(t *testing.T) {
	m := model{allTools: tools.All()}
	for _, tool := range tools.All() {
		if got := m.findTool(tool.Name); got == nil {
			t.Errorf("findTool(%q) = nil, want tool", tool.Name)
		} else if got.Name != tool.Name {
			t.Errorf("findTool(%q).Name = %q, want %q", tool.Name, got.Name, tool.Name)
		}
	}
	if got := m.findTool("nonexistent"); got != nil {
		t.Errorf("findTool(nonexistent) = %v, want nil", got)
	}
}

func TestNewSpinner(_ *testing.T) {
	s := newSpinner()
	_ = s // spinner is created without panic; structural check is sufficient
}

func TestBannerContainsVersion(t *testing.T) {
	got := banner("1.2.3")
	if !strings.Contains(got, "1.2.3") {
		t.Errorf("banner output does not contain version: %q", got)
	}
	if !strings.Contains(got, "ferry your AI tools") {
		t.Errorf("banner output does not contain tagline: %q", got)
	}
}

func TestBannerWithoutVersion(t *testing.T) {
	got := banner("")
	if !strings.Contains(got, "ferry your AI tools") {
		t.Errorf("banner output does not contain tagline: %q", got)
	}
}

func TestSetStatusAndClearStatus(t *testing.T) {
	m := model{}
	m.setStatus(statusOK, "done")
	if m.status != "done" || m.statusLvl != statusOK {
		t.Errorf("after setStatus: status=%q lvl=%v, want done/OK", m.status, m.statusLvl)
	}
	m.clearStatus()
	if m.status != "" || m.statusLvl != statusInfo {
		t.Errorf("after clearStatus: status=%q lvl=%v, want empty/info", m.status, m.statusLvl)
	}
}

func TestEscAndQKeyNavigation(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "v1.0.0")

	testKeys := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}},
		{"q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}},
	}

	// In viewTools, esc or q key should return tea.Quit.
	for _, tk := range testKeys {
		m.view = viewTools
		m2, cmd := m.Update(tk.msg)
		if cmd == nil {
			t.Errorf("Update with %q in viewTools returned nil cmd, want tea.Quit", tk.name)
		} else {
			msg := cmd()
			if _, ok := msg.(tea.QuitMsg); !ok {
				t.Errorf("Update with %q in viewTools cmd = %v, want tea.QuitMsg", tk.name, msg)
			}
		}
		_ = m2
	}

	// In viewProfiles, esc or q key should back out to viewTools.
	m.tool = m.allTools[0]
	for _, tk := range testKeys {
		m.view = viewProfiles
		m2, _ := m.Update(tk.msg)
		updatedModel := m2.(model)
		if updatedModel.view != viewTools {
			t.Errorf("Update with %q in viewProfiles view = %v, want viewTools", tk.name, updatedModel.view)
		}
	}
}

func TestEnterKeyInToolsAndProfiles(t *testing.T) {
	st := openTestCatalog(t, true)
	m := newModel(st, "v0.0.0")
	m.width = 100
	m.height = 30
	m.resize()

	// 1. Enter on a detected tool in viewTools opens viewProfiles.
	m.view = viewTools
	detectedIdx := -1
	for i, it := range m.list.Items() {
		if row, ok := it.(item); ok {
			tl := m.findTool(row.value)
			if tl != nil && tl.Detected != nil && tl.Detected() {
				detectedIdx = i
				break
			}
		}
	}
	if detectedIdx >= 0 {
		m.list.Select(detectedIdx)
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		updated := m2.(model)
		if updated.view != viewProfiles {
			t.Errorf("Enter on tool in viewTools = %v, want viewProfiles", updated.view)
		}

		// 2. Enter on '＋ Add new binding…' in viewProfiles opens viewEditForm.
		addIdx := -1
		for i, it := range updated.list.Items() {
			if row, ok := it.(item); ok && row.value == addSentinel {
				addIdx = i
				break
			}
		}
		if addIdx >= 0 {
			updated.list.Select(addIdx)
			m3, _ := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updatedForm := m3.(model)
			if updatedForm.view != viewEditForm {
				t.Errorf("Enter on Add in viewProfiles = %v, want viewEditForm", updatedForm.view)
			}
		}
	}
}

func TestAKeyInProfilesOpensAddForm(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "v0.0.0")
	m.tool = m.allTools[0]
	m.view = viewProfiles

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	updated := m2.(model)
	if updated.view != viewEditForm {
		t.Errorf("pressing 'a' in viewProfiles view = %v, want viewEditForm", updated.view)
	}
	if updated.wiz.edit {
		t.Error("wiz.edit should be false for a new binding")
	}
}

func TestDuplicateFlowNamesAndCopiesBinding(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "v0.0.0")
	m.tool = m.findTool("claude")
	m.view = viewProfiles

	if _, err := catalog.StoreBinding(st, m.tool, nil, "work", "https://gateway.example", "sk-test", "kimi-k2", []string{"kimi-k2", "glm-4.6"}, true, nil); err != nil {
		t.Fatal(err)
	}
	m.loadProfiles("work")

	// "c" and "x" both open the destination chooser; this tool leads the list.
	var chosen tea.Model
	for _, key := range []string{"c", "x"} {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if got := m2.(model).view; got != viewCopyTool {
			t.Fatalf("pressing %q = %v, want viewCopyTool", key, got)
		}
		if row, ok := m2.(model).list.SelectedItem().(item); !ok || row.value != "claude" {
			t.Fatalf("first destination row = %+v, want this tool (claude)", row)
		}
		chosen = m2
	}

	// Destination: another tool → a naming step with a prefilled "-copy" name.
	updated := chosen.(model)
	updated.width = 100
	updated.height = 30
	updated.resize()
	var downModel tea.Model
	for range 8 {
		downModel, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
		updated = downModel.(model)
		if row, ok := updated.list.SelectedItem().(item); ok && row.value == "opencode" {
			break
		}
	}
	if row, ok := updated.list.SelectedItem().(item); !ok || row.value != "opencode" {
		t.Fatal("could not move cursor to opencode")
	}
	enteredModel, _ := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	naming := enteredModel.(model)
	if naming.view != viewDupName {
		t.Fatalf("Enter on target tool = %v, want viewDupName; status = %q", naming.view, naming.status)
	}
	if got := naming.input.Value(); got != "work-copy" {
		t.Fatalf("prefilled name = %q, want work-copy", got)
	}
	naming.input.SetValue("shared-gateway")
	finalModel, _ := naming.Update(tea.KeyMsg{Type: tea.KeyEnter})
	final := finalModel.(model)
	if final.view != viewProfiles {
		t.Fatalf("naming submit = %v, want viewProfiles; status = %q", final.view, final.status)
	}

	copied, found, err := st.BindingByName("opencode", "shared-gateway")
	if err != nil || !found {
		t.Fatalf("copied binding missing: found=%v err=%v", found, err)
	}
	if _, found, err := st.BindingByName("opencode", "work-copy"); found || err != nil {
		t.Fatalf("prefilled name must not be written when the user renames: found=%v err=%v", found, err)
	}
	if slugs, err := st.ModelSlugs(copied.Models); err != nil || len(slugs) != 2 {
		t.Fatalf("copied models = %v, err=%v; want two models", slugs, err)
	}
}

// TestDuplicateWithinToolSharesCredential drives the first destination row
// ("This tool") through the naming step: the clone must reuse the credential row
// and land under the cursor, while the source binding stays active.
func TestDuplicateWithinToolSharesCredential(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "v0.0.0")
	m.tool = m.findTool("claude")
	m.view = viewProfiles

	work, err := catalog.StoreBinding(st, m.tool, nil, "work", "https://gateway.example", "sk-test", "kimi-k2", []string{"kimi-k2", "glm-4.6"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Activate(work.ID); err != nil {
		t.Fatal(err)
	}
	m.loadProfiles("work")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(model)
	// First row is "This tool"; enter accepts it and lands on the naming step.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.view != viewDupName {
		t.Fatalf("this-tool destination = %v, want viewDupName", m.view)
	}
	if got := m.input.Value(); got != "work-copy" {
		t.Fatalf("prefilled name = %q, want work-copy", got)
	}
	m.input.SetValue("work-backup")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.view != viewProfiles {
		t.Fatalf("naming submit = %v, want viewProfiles; status = %q", m.view, m.status)
	}

	clone, found, err := st.BindingByName("claude", "work-backup")
	if err != nil || !found {
		t.Fatalf("clone missing: found=%v err=%v", found, err)
	}
	if clone.CredentialID != work.CredentialID {
		t.Errorf("clone credential = %s, want shared %s", clone.CredentialID, work.CredentialID)
	}
	if active, found, err := st.Active("claude"); err != nil || !found || active.Name != "work" {
		t.Errorf("active binding changed by cloning: %+v found=%v err=%v", active, found, err)
	}
	if it, ok := m.list.SelectedItem().(item); !ok || it.value != "work-backup" {
		t.Errorf("cursor after clone = %+v, want the new clone", it)
	}
}

// TestDuplicateRejectsTakenName cancels nothing but writes nothing either: a
// name already used inside the destination tool is refused with the catalog's
// own message, and the naming step stays open.
func TestDuplicateRejectsTakenName(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "v0.0.0")
	m.tool = m.findTool("claude")
	m.view = viewProfiles

	if _, err := catalog.StoreBinding(st, m.tool, nil, "work", "https://gateway.example", "sk-test", "kimi-k2", []string{"kimi-k2"}, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.StoreBinding(st, m.tool, nil, "taken", "https://gateway.example", "sk-test", "kimi-k2", []string{"kimi-k2"}, true, nil); err != nil {
		t.Fatal(err)
	}
	m.loadProfiles("work")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // This tool
	m = next.(model)
	m.input.SetValue("taken")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.view != viewDupName {
		t.Fatalf("view after refused name = %v, want viewDupName (stay on the step)", m.view)
	}
	if m.statusLvl != statusErr || !strings.Contains(m.status, "taken") {
		t.Errorf("status = %q (lvl %v), want an error naming the conflict", m.status, m.statusLvl)
	}
	saved, err := st.Bindings("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 {
		t.Errorf("bindings after refused name = %d, want the original two", len(saved))
	}
}

// pickerModel builds a picker-view model over a fetched list, sandboxed so no real
// config is touched. The tool is one that can be given a model list (ModelMenu set),
// since that's what the curation keys are for.
func pickerModel(t *testing.T, fetched []string) *model {
	t.Helper()
	st := openTestCatalog(t, false)
	m := newModel(st, "v0.0.0")
	m.tool = toolWithModelMenu(t, &m)
	m.view = viewPickModel
	m.showModels(fetched)
	return &m
}

// toolWithModelMenu returns the first registered tool whose config can hold a model list.
func toolWithModelMenu(t *testing.T, m *model) *tools.Tool {
	t.Helper()
	for _, tool := range m.allTools {
		if tool.ModelMenu != "" {
			return tool
		}
	}
	t.Fatal("no registered tool exposes a model menu")
	return nil
}

func TestToggleModelBuildsCuratedListInOrder(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6", "deepseek-v3"})

	m.toggleModel("glm-4.6")
	m.toggleModel("kimi-k2")
	if got := strings.Join(m.wiz.models, ","); got != "glm-4.6,kimi-k2" {
		t.Errorf("models = %q, want first-seen order glm-4.6,kimi-k2", got)
	}
	if !m.modelSelected("kimi-k2") || m.modelSelected("deepseek-v3") {
		t.Error("modelSelected disagrees with the toggled list")
	}

	// Toggling again unchecks, leaving the rest of the list untouched.
	m.toggleModel("glm-4.6")
	if got := strings.Join(m.wiz.models, ","); got != "kimi-k2" {
		t.Errorf("models after untoggle = %q, want kimi-k2", got)
	}
}

// TestPickerModelsFallsBackToFetchedList locks in the pre-existing behavior: picking
// one model without checking any rows still registers everything the endpoint offers,
// so /model inside the tool isn't reduced to a single row.
func TestPickerModelsFallsBackToFetchedList(t *testing.T) {
	fetched := []string{"kimi-k2", "glm-4.6"}
	m := pickerModel(t, fetched)

	if got := strings.Join(m.pickerModels(), ","); got != strings.Join(fetched, ",") {
		t.Errorf("pickerModels() = %q, want the whole fetched list %q", got, fetched)
	}
	m.toggleModel("glm-4.6")
	if got := strings.Join(m.pickerModels(), ","); got != "glm-4.6" {
		t.Errorf("pickerModels() = %q, want just the curated selection", got)
	}
}

// TestShowModelsDropsCheckedIDsNoLongerOffered guards a re-fetch against a different
// endpoint: a checked id the new catalog doesn't list must not stay in the curated set,
// or the tool would be handed a model its gateway can't serve.
func TestShowModelsDropsCheckedIDsNoLongerOffered(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6"})
	m.toggleModel("kimi-k2")
	m.toggleModel("glm-4.6")

	m.showModels([]string{"glm-4.6", "qwen3-max"})
	if got := strings.Join(m.wiz.models, ","); got != "glm-4.6" {
		t.Errorf("models after re-fetch = %q, want only the still-offered glm-4.6", got)
	}
}

func TestSpaceTogglesHighlightedModelWithoutMovingCursor(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6", "deepseek-v3"})

	// Land on a real model row rather than an action row or the trailing skip row.
	target := ""
	for i, it := range m.list.Items() {
		if row, ok := it.(item); ok && !isSentinel(row.value) {
			m.list.Select(i)
			target = row.value
			break
		}
	}
	if target == "" {
		t.Fatal("no model row in the picker")
	}
	before := m.list.Index()

	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeySpace})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("updatePickModel returned %T, want model", next)
	}
	if !got.modelSelected(target) {
		t.Errorf("space did not check %q into the curated list (models=%v)", target, got.wiz.models)
	}
	if got.list.Index() != before {
		t.Errorf("cursor moved to %d, want it to stay on %d", got.list.Index(), before)
	}
	// Space must not leak into the search query, or the list would filter to nothing.
	if got.modelFilter != "" {
		t.Errorf("modelFilter = %q, want space to toggle rather than search", got.modelFilter)
	}
}

func TestSubmitFormRequiresModel(t *testing.T) {
	m := model{tool: &tools.Tool{Name: "claude", Title: "Claude"}, wiz: wizard{}}
	m.loadEditForm()
	m.formInputs[focusName].SetValue("my-binding")
	m.formInputs[focusURL].SetValue("https://api.anthropic.com")
	m.formInputs[focusToken].SetValue("sk-test")
	next, _ := m.submitForm()
	got := next.(model)
	if !strings.Contains(got.status, "Model is required") {
		t.Fatalf("submit without model status = %q, want Model is required", got.status)
	}

	m.wiz.model = "claude-3-5-sonnet"
	if m.wiz.model != "claude-3-5-sonnet" {
		t.Fatalf("model = %q, want claude-3-5-sonnet", m.wiz.model)
	}
}

func TestModelRowTitleMarksCheckedRows(t *testing.T) {
	if got := modelRowTitle("m", true, 0); got != "[✓] m · context unknown" {
		t.Errorf("checked modelRowTitle = %q, want %q", got, "[✓] m · context unknown")
	}
	if got := modelRowTitle("m", false, 200000); got != "[ ] m · context 200,000" {
		t.Errorf("unchecked modelRowTitle = %q, want %q", got, "[ ] m · context 200,000")
	}
}

// TestFailedFetchFallsBackToManualEntry is the whole point of the manual path: an
// endpoint that serves chat but no /v1/models must still let you register model ids,
// instead of dropping the model override and moving on.
func TestFailedFetchFallsBackToManualEntry(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x", model: "kimi-k2",
		models: []string{"kimi-k2", "glm-4.6"}}

	next, _ := m.applyFetched(fetchedMsg{err: errFetchFailed})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("applyFetched returned %T, want model", next)
	}
	if got.view != viewReviewModels {
		t.Errorf("view = %v, want the model list editor", got.view)
	}
	if got.statusLvl != statusErr || !strings.Contains(got.status, "add the model ids") {
		t.Errorf("status = %q (level %v), want the error to point at manual entry", got.status, got.statusLvl)
	}
	if got.reviewCursor != len(got.wiz.models) {
		t.Errorf("cursor = %d, want the add row at %d", got.reviewCursor, len(got.wiz.models))
	}
	if strings.Join(got.wiz.models, ",") != "kimi-k2,glm-4.6" {
		t.Errorf("models = %v, want the binding's current ids kept", got.wiz.models)
	}
}

// addModelInline focuses the list's inline add row, types ids, and commits them.
func addModelInline(t *testing.T, m model, ids string) model {
	t.Helper()
	next, _ := m.startInlineModelAdd()
	typed := next.(model)
	typed.input.SetValue(ids)
	next, _ = typed.commitInlineModelAdd()
	got, ok := next.(model)
	if !ok {
		t.Fatalf("commitInlineModelAdd returned %T, want model", next)
	}
	return got
}

// TestInlineModelAddDoesNotLeaveTheList pins the interaction: enter focuses the
// add field in place, and typing or committing never changes the view.
func TestInlineModelAddDoesNotLeaveTheList(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x"}
	m.view = viewReviewModels
	m.reviewCursor = 0

	next, _ := m.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	typed := next.(model)
	if typed.view != viewReviewModels || !typed.addingModel {
		t.Fatalf("enter: view = %v adding = %v, want the inline field on the list", typed.view, typed.addingModel)
	}
	typed.input.SetValue("glm-4.6, llama3:8b")

	next, _ = typed.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.view != viewReviewModels || got.addingModel {
		t.Fatalf("commit: view = %v adding = %v, want the list without the field", got.view, got.addingModel)
	}
	if strings.Join(got.wiz.models, ",") != "glm-4.6,llama3:8b" {
		t.Fatalf("models = %v, want both ids added", got.wiz.models)
	}
	if got.reviewCursor != 1 {
		t.Errorf("cursor = %d, want the last added row", got.reviewCursor)
	}
}

// TestInlineCtxEditDoesNotLeaveTheList pins direct ctx editing: w focuses the
// cell in place, enter saves, and the view never changes.
func TestInlineCtxEditDoesNotLeaveTheList(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{
		endpoint: "https://relay.example/v1", key: "sk-x",
		models: []string{"glm-5.3"}, model: "glm-5.3",
		windows: map[string]int{"glm-5.3": 600_000},
	}
	m.view = viewReviewModels
	m.reviewCursor = 0

	next, _ := m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	editing := next.(model)
	if editing.view != viewReviewModels || !editing.editingCtx {
		t.Fatalf("w: view = %v editing = %v, want the inline field", editing.view, editing.editingCtx)
	}
	if editing.input.Value() != "600000" {
		t.Fatalf("input = %q, want the current count", editing.input.Value())
	}

	editing.input.SetValue("350k")
	next, _ = editing.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.view != viewReviewModels || got.editingCtx {
		t.Fatalf("enter: view = %v editing = %v, want the list", got.view, got.editingCtx)
	}
	if got.wiz.windows["glm-5.3"] != 350_000 || !got.wiz.manualWindows["glm-5.3"] {
		t.Errorf("window = %d manual=%v, want 350000 manual",
			got.wiz.windows["glm-5.3"], got.wiz.manualWindows["glm-5.3"])
	}
}

// TestInlineCtxEditCancelAndNoOp covers the three quiet paths: esc keeps the old
// value, confirming it keeps its source, and bad input leaves the field open.
func TestInlineCtxEditCancelAndNoOp(t *testing.T) {
	newCtxModel := func() model {
		m := *pickerModel(t, nil)
		m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x",
			models: []string{"m"}, model: "m", windows: map[string]int{"m": 200_000}}
		m.view = viewReviewModels
		return m
	}

	m := newCtxModel()
	next, _ := m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	editing := next.(model)
	editing.input.SetValue("1m")
	next, _ = editing.updateReviewModels(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(model); got.wiz.windows["m"] != 200_000 || got.wiz.manualWindows["m"] || got.editingCtx {
		t.Fatalf("esc changed the window: %+v", got.wiz)
	}

	m = newCtxModel()
	next, _ = m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	editing = next.(model)
	next, _ = editing.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(model); got.wiz.windows["m"] != 200_000 || got.wiz.manualWindows["m"] || got.editingCtx {
		t.Fatalf("confirming the current value changed it: %+v", got.wiz)
	}

	m = newCtxModel()
	next, _ = m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	editing = next.(model)
	editing.input.SetValue("lots")
	next, _ = editing.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(model); !got.editingCtx || got.statusLvl != statusErr {
		t.Fatalf("invalid input: editing = %v status = %q", got.editingCtx, got.status)
	}
}

// TestInlineModelEditDoesNotLeaveTheList pins direct row editing ("e"): the id
// is focused in place, enter saves and chains straight into the ctx field, and the
// window, its manual flag, and the default model all follow the new id.
func TestInlineModelEditDoesNotLeaveTheList(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{
		endpoint: "https://relay.example/v1", key: "sk-x",
		models: []string{"glm-5.3", "kimi-k2"}, model: "glm-5.3",
		windows:       map[string]int{"glm-5.3": 600_000},
		manualWindows: map[string]bool{"glm-5.3": true},
	}
	m.view = viewReviewModels
	m.reviewCursor = 0

	next, _ := m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	editing := next.(model)
	if editing.view != viewReviewModels || !editing.editingID {
		t.Fatalf("e: view = %v editing = %v, want the inline id field", editing.view, editing.editingID)
	}
	if editing.input.Value() != "glm-5.3" {
		t.Fatalf("input = %q, want the current id prefilled", editing.input.Value())
	}

	editing.input.SetValue("glm-5.4")
	next, _ = editing.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	ctx := next.(model)
	if ctx.view != viewReviewModels || ctx.editingID {
		t.Fatalf("enter: view = %v editing id = %v, want the id committed", ctx.view, ctx.editingID)
	}
	if strings.Join(ctx.wiz.models, ",") != "glm-5.4,kimi-k2" {
		t.Fatalf("models = %v, want the row renamed in place", ctx.wiz.models)
	}
	if ctx.wiz.model != "glm-5.4" {
		t.Errorf("default model = %q, want it to follow the rename", ctx.wiz.model)
	}
	if ctx.wiz.windows["glm-5.4"] != 600_000 || ctx.wiz.windows["glm-5.3"] != 0 {
		t.Errorf("windows = %v, want the window carried to the new id", ctx.wiz.windows)
	}
	if !ctx.wiz.manualWindows["glm-5.4"] || ctx.wiz.manualWindows["glm-5.3"] {
		t.Errorf("manualWindows = %v, want the flag carried to the new id", ctx.wiz.manualWindows)
	}
	// The edit chains into the ctx field for the new id, prefilled with its window.
	if !ctx.editingCtx || ctx.editTarget != "glm-5.4" || ctx.input.Value() != "600000" {
		t.Fatalf("after id commit: editingCtx = %v target = %q input = %q, want the ctx field for glm-5.4",
			ctx.editingCtx, ctx.editTarget, ctx.input.Value())
	}

	next, _ = ctx.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(model); got.editingCtx || got.view != viewReviewModels {
		t.Fatalf("ctx enter: editingCtx = %v view = %v, want the list", got.editingCtx, got.view)
	}
}

// TestInlineModelEditCancelAndGuards covers the quiet and refused paths: esc keeps
// the old id, an unchanged id still chains into the ctx field, and a duplicate id
// leaves the id field open with an error.
func TestInlineModelEditCancelAndGuards(t *testing.T) {
	newEditModel := func() model {
		m := *pickerModel(t, nil)
		m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x",
			models: []string{"m", "other"}, model: "m", windows: map[string]int{"m": 200_000}}
		m.view = viewReviewModels
		m.reviewCursor = 0
		return m
	}

	m := newEditModel()
	next, _ := m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	editing := next.(model)
	editing.input.SetValue("renamed")
	next, _ = editing.updateReviewModels(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(model); strings.Join(got.wiz.models, ",") != "m,other" || got.editingID {
		t.Fatalf("esc changed the list: %v editing=%v", got.wiz.models, got.editingID)
	}

	m = newEditModel()
	next, _ = m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	editing = next.(model)
	next, _ = editing.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(model); strings.Join(got.wiz.models, ",") != "m,other" || got.editingID || !got.editingCtx || got.editTarget != "m" {
		t.Fatalf("unchanged id: models = %v editing=%v ctx=%v target=%q, want the ctx field for the same id",
			got.wiz.models, got.editingID, got.editingCtx, got.editTarget)
	}

	m = newEditModel()
	next, _ = m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	editing = next.(model)
	editing.input.SetValue("other")
	next, _ = editing.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(model); !got.editingID || got.statusLvl != statusErr {
		t.Fatalf("duplicate id: editing = %v status = %q, want the field open with an error", got.editingID, got.status)
	}
}

// TestDeleteLastModelEmptiesTheList pins that removing the only row is allowed:
// the list lands on its add row, enter starts the add field (not an error), and
// the next id can be typed straight in.
func TestDeleteLastModelEmptiesTheList(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x",
		models: []string{"glm-5.3"}, model: "glm-5.3", windows: map[string]int{"glm-5.3": 600_000}}
	m.view = viewReviewModels
	m.reviewCursor = 0

	next, _ := m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := next.(model)
	if len(got.wiz.models) != 0 {
		t.Fatalf("models = %v, want the last row removable", got.wiz.models)
	}
	if got.reviewCursor != 0 {
		t.Errorf("cursor = %d, want the add row", got.reviewCursor)
	}
	if got.statusLvl != statusOK {
		t.Errorf("status = %q (%v), want an ok removal", got.status, got.statusLvl)
	}

	next, _ = got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if after := next.(model); after.view != viewReviewModels || !after.addingModel {
		t.Fatalf("enter on the emptied list: view = %v adding = %v, want the inline add field", after.view, after.addingModel)
	}

	refilled := addModelInline(t, next.(model), "kimi-k2")
	if strings.Join(refilled.wiz.models, ",") != "kimi-k2" || refilled.wiz.model != "kimi-k2" {
		t.Fatalf("models = %v default = %q, want the new id registered as default", refilled.wiz.models, refilled.wiz.model)
	}
}

// TestManualEntryRegistersTypedIDs covers the typed-list path: every id is registered,
// the first becomes the default model, and the commit lands back on the form (one level
// up) rather than saving the binding outright.
func TestManualEntryRegistersTypedIDs(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x", name: "relay", edit: true}
	m.view = viewReviewModels
	m.reviewCursor = 0

	got := addModelInline(t, *m, "glm-4.6, kimi-k2 ,, deepseek-v3")
	if got.view != viewReviewModels {
		t.Errorf("view = %v, want viewReviewModels", got.view)
	}
	if got.wiz.model != "glm-4.6" {
		t.Errorf("default model = %q, want the first typed id", got.wiz.model)
	}
	if strings.Join(got.wiz.models, ",") != "glm-4.6,kimi-k2,deepseek-v3" {
		t.Errorf("models = %v, want every typed id registered", got.wiz.models)
	}
}

// TestManualEntryFromFormReturnsToForm pins the navigation contract the user sees:
// [ Add / Edit Model List ] opened from the single-page form must come back to that
// form, with the ids in place and [ Save ] still to press — not skip a level and store
// the binding.
func TestManualEntryFromFormReturnsToForm(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{name: "relay", endpoint: "https://relay.example/v1", key: "sk-x"}
	m.fromForm = true
	m.view = viewReviewModels
	m.reviewCursor = 0

	got := addModelInline(t, *m, "glm-4.6, kimi-k2")
	if got.view != viewReviewModels {
		t.Errorf("view = %v, want viewReviewModels", got.view)
	}
	if strings.Join(got.wiz.models, ",") != "glm-4.6,kimi-k2" || got.wiz.model != "glm-4.6" {
		t.Errorf("models = %v, model = %q, want the typed ids with the first as default", got.wiz.models, got.wiz.model)
	}
	if !strings.Contains(got.View(), "Models and context windows:") {
		t.Errorf("manual models must enter the review table:\n%s", got.View())
	}

	// Pressing Enter in review table confirms and returns to form
	nextForm, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	gotForm := nextForm.(model)
	if gotForm.view != viewEditForm {
		t.Errorf("view = %v, want viewEditForm after confirming review", gotForm.view)
	}
}

// TestManualEntryAddsRowsWithoutParsingContext pins the row-based model list:
// ids are added verbatim (colons and @ included), and the context window is a
// column edited separately instead of a suffix crammed into the input line.
func TestManualEntryAddsRowsWithoutParsingContext(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{name: "work", endpoint: "https://gateway.example/v1", key: "sk-secret"}
	m.view = viewReviewModels
	m.reviewCursor = 0

	got := addModelInline(t, *m, "llama3:8b, org/model@tag, glm-5.3")
	if got.view != viewReviewModels {
		t.Fatalf("view = %v, want viewReviewModels", got.view)
	}
	want := []string{"llama3:8b", "org/model@tag", "glm-5.3"}
	if strings.Join(got.wiz.models, "|") != strings.Join(want, "|") {
		t.Fatalf("models = %v, want the ids verbatim", got.wiz.models)
	}
	if got.reviewCursor != 2 {
		t.Errorf("reviewCursor = %d, want the last added row", got.reviewCursor)
	}
	// A colon in the id is part of the id, not a window suffix.
	if got.wiz.windows["llama3:8b"] != models.DefaultContextWindow("llama3:8b") {
		t.Errorf("llama3:8b window = %d, want its derived default", got.wiz.windows["llama3:8b"])
	}
	if got.wiz.manualWindows["llama3:8b"] {
		t.Error("llama3:8b must not be marked manual")
	}

	viewOut := got.View()
	if !strings.Contains(viewOut, "Models and context windows:") {
		t.Errorf("view missing header: %s", viewOut)
	}
	if !strings.Contains(viewOut, "llama3:8b") || !strings.Contains(viewOut, "org/model@tag") {
		t.Errorf("view missing model rows: %s", viewOut)
	}
	if !strings.Contains(viewOut, "＋ Add model…") {
		t.Errorf("view missing the add row: %s", viewOut)
	}

	// a focuses the add row in place; esc cancels without leaving the list.
	more, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	add := more.(model)
	if add.view != viewReviewModels || !add.addingModel {
		t.Fatalf("a: view = %v adding = %v, want the inline field on the list", add.view, add.addingModel)
	}
	if add.input.Value() != "" {
		t.Fatalf("inline field = %q, want it empty", add.input.Value())
	}
	esc, _ := add.updateReviewModels(tea.KeyMsg{Type: tea.KeyEsc})
	if back := esc.(model); back.view != viewReviewModels || back.addingModel {
		t.Fatalf("esc: view = %v adding = %v, want the list without the field", back.view, back.addingModel)
	}

	// w edits the highlighted row's ctx in place, without changing views.
	ctx, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if c := ctx.(model); c.view != viewReviewModels || !c.editingCtx || c.editTarget != "glm-5.3" {
		t.Fatalf("w: view = %v editing = %v target = %q, want the inline ctx field for glm-5.3",
			c.view, c.editingCtx, c.editTarget)
	}
}

// TestManualEntryDedupesRows checks repeated ids in one paste and across adds.
func TestManualEntryDedupesRows(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{endpoint: "https://gateway.example/v1", key: "sk-secret"}
	m.view = viewReviewModels
	m.reviewCursor = 0

	got := addModelInline(t, *m, "a, b, a")
	if strings.Join(got.wiz.models, ",") != "a,b" {
		t.Fatalf("models = %v, want duplicates dropped", got.wiz.models)
	}
	if !strings.Contains(got.status, "1 already listed") {
		t.Errorf("status = %q, want the duplicate called out", got.status)
	}

	again, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	add := again.(model)
	add.input.SetValue("a")
	dup, _ := add.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	gotDup := dup.(model)
	if strings.Join(gotDup.wiz.models, ",") != "a,b" {
		t.Fatalf("models after re-adding = %v, want unchanged", gotDup.wiz.models)
	}
	if !strings.Contains(gotDup.status, "already listed: a") {
		t.Errorf("status = %q, want the already-listed notice", gotDup.status)
	}
}

// TestFormCursorSurvivesModelSubScreens: coming back from a sub-screen must leave the
// cursor on the row that was left — manual ids on the manual row (both on enter and on
// esc), the picker on the fetch row — instead of resetting it to the name row.
func TestFormCursorSurvivesModelSubScreens(t *testing.T) {
	manual := pickerModel(t, nil)
	manual.wiz = wizard{name: "relay", endpoint: "https://relay.example/v1", key: "sk-x"}
	manual.fromForm = true
	manual.view = viewEditForm
	started, _ := manual.startManualModels()
	added := addModelInline(t, started.(model), "glm-4.6")
	if added.view != viewReviewModels {
		t.Errorf("manual add: view=%v, want viewReviewModels", added.view)
	}
	nextForm, _ := added.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if kept := nextForm.(model); kept.view != viewEditForm || kept.formFocus != focusModels {
		t.Errorf("manual confirm: view=%v focus=%d, want viewEditForm focusModels", kept.view, kept.formFocus)
	}

	escManual := pickerModel(t, nil)
	escManual.wiz = wizard{name: "relay", endpoint: "https://relay.example/v1", key: "sk-x"}
	escManual.fromForm = true
	escManual.reviewSource = viewEditForm
	escManual.view = viewReviewModels
	escNext, _ := escManual.updateReviewModels(tea.KeyMsg{Type: tea.KeyEsc})
	if got := escNext.(model); got.formFocus != focusModels {
		t.Errorf("manual esc: formFocus = %d, want focusModels", got.formFocus)
	}

	m := pickerModel(t, []string{"glm-4.6", "kimi-k2"})
	m.wiz = wizard{name: "relay", endpoint: "https://relay.example/v1", key: "sk-x"}
	m.fromForm = true
	escNext, _ = m.updatePickModel(tea.KeyMsg{Type: tea.KeyEsc})
	if got := escNext.(model); got.formFocus != focusModels {
		t.Errorf("picker esc: formFocus = %d, want focusModels", got.formFocus)
	}

	mEnter := pickerModel(t, []string{"glm-4.6", "kimi-k2"})
	mEnter.wiz = wizard{name: "relay", endpoint: "https://relay.example/v1", key: "sk-x"}
	mEnter.fromForm = true
	mEnter.toggleModel("glm-4.6")
	enterNext, _ := mEnter.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	reviewGot := enterNext.(model)
	if reviewGot.view != viewReviewModels {
		t.Errorf("picker enter: view = %v, want viewReviewModels", reviewGot.view)
	}
	nextForm, _ = reviewGot.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if formNext := nextForm.(model); formNext.formFocus != focusModels {
		t.Errorf("review confirm: formFocus = %d, want focusModels", formNext.formFocus)
	}
}

// TestManualEntryFromStepFlowAdvancesToName keeps the wizard path working: when manual
// entry is reached without a form behind it (a fetch failure after the key screen),
// committing the ids still advances to naming the binding.
func TestManualEntryFromStepFlowAdvancesToName(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x"}
	m.view = viewReviewModels
	m.reviewCursor = 0

	got := addModelInline(t, *m, "glm-4.6")
	if got.view != viewReviewModels {
		t.Errorf("view = %v, want viewReviewModels", got.view)
	}
	nextName, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	if gotName := nextName.(model); gotName.view != viewAddName {
		t.Errorf("view after confirming models = %v, want viewAddName", gotName.view)
	}
}

func TestSpaceRejectedForToolWithoutModelMenu(t *testing.T) {
	m := pickerModel(t, []string{"gpt-5.5", "gpt-5.4"})
	m.tool = &tools.Tool{Title: "No model menu"}
	m.renderModels()
	for i, it := range m.list.Items() {
		if row, ok := it.(item); ok && !isSentinel(row.value) {
			m.list.Select(i)
			break
		}
	}

	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeySpace})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("updatePickModel returned %T, want model", next)
	}
	if len(got.wiz.models) != 0 {
		t.Errorf("models = %v, want no curation for a tool that can't register a list", got.wiz.models)
	}
	if !strings.Contains(got.status, "can't be given a model list") {
		t.Errorf("status = %q, want an explanation instead of silent collection", got.status)
	}
}

func TestCodexPickerRegistersMultipleModels(t *testing.T) {
	m := pickerModel(t, []string{"gpt-5.5", "gpt-5.4"})
	m.tool = tools.Find("codex")
	if m.tool.ModelMenu != "/model" {
		t.Fatalf("Codex model menu = %q", m.tool.ModelMenu)
	}
	m.wiz.models = []string{"gpt-5.5"}
	m.wiz.model = "gpt-5.5"
	m.renderModels()

	view := m.View()
	if strings.Contains(view, "Done") {
		t.Errorf("Codex picker status must not mention Done: %q", view)
	}
	for _, it := range m.list.Items() {
		row, ok := it.(item)
		if !ok {
			continue
		}
		if strings.Contains(row.title, "•") {
			t.Errorf("Codex picker must not show a bullet mark on %q", row.title)
		}
		if row.value == "gpt-5.5" && !strings.HasPrefix(row.title, "[✓] ") {
			t.Errorf("checked Codex row = %q, want [✓] prefix", row.title)
		}
		if row.value == "gpt-5.4" && !strings.HasPrefix(row.title, "[ ] ") {
			t.Errorf("unchecked Codex row = %q, want [ ] prefix", row.title)
		}
	}
	if strings.Contains(m.list.Title, "single model only") {
		t.Errorf("Codex picker title still restricts model selection: %q", m.list.Title)
	}
	m.toggleModel("gpt-5.4")
	if got := strings.Join(m.pickerModels(), ","); got != "gpt-5.5,gpt-5.4" {
		t.Fatalf("registered models = %q", got)
	}
	m.wiz.models = nil
	if got := strings.Join(m.pickerModels(), ","); got != "gpt-5.5,gpt-5.4" {
		t.Fatalf("unselected picker should register fetched list, got %q", got)
	}
}

func TestCodexFinishAddKeepsMultipleModels(t *testing.T) {
	cat := openTestCatalog(t, false)
	ui := newModel(cat, "test")
	ui.tool = tools.Find("codex")
	ui.wiz = wizard{
		endpoint: "https://gateway.example/v1",
		key:      "sk-test",
		model:    "glm-5.3-flash",
		models:   []string{"glm-5.3-flash", "gpt-5.6-luna"},
	}
	result, _ := ui.finishAdd("multi")
	if updated := result.(model); updated.statusLvl == statusErr {
		t.Fatal(updated.status)
	}
	binding, found, err := cat.BindingByName("codex", "multi")
	if err != nil || !found {
		t.Fatalf("binding missing: found=%v err=%v", found, err)
	}
	if slugs, err := cat.ModelSlugs(binding.Models); err != nil || strings.Join(slugs, ",") != "glm-5.3-flash,gpt-5.6-luna" {
		t.Fatalf("saved models = %v, err=%v", slugs, err)
	}
}

func TestPickerNote(t *testing.T) {
	m := pickerModel(t, nil)
	if note := m.pickerNote(); note != "" {
		t.Errorf("pickerNote() with no models = %q, want empty", note)
	}

	m.wiz.models = []string{"kimi-k2", "glm-4.6"}
	m.tool = &tools.Tool{Title: "Claude", ModelMenu: "/model"}
	if got := m.pickerNote(); got != "2 model(s) offered in /model" {
		t.Errorf("pickerNote() = %q, want %q", got, "2 model(s) offered in /model")
	}

	m.tool = &tools.Tool{Title: "Codex", ModelMenu: ""}
	if got := m.pickerNote(); got != "2 model(s) offered in the tool's own picker" {
		t.Errorf("pickerNote() for tool without menu = %q, want %q", got, "2 model(s) offered in the tool's own picker")
	}
}

func TestPickerEnterFinishesOnModelRow(t *testing.T) {
	// Editing an existing binding: enter on a model row returns to the form, and
	// the stored model becomes the first checked id.
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6", "deepseek-v3"})
	m.wiz.edit = true
	m.toggleModel("glm-4.6")
	m.toggleModel("kimi-k2")
	m.wiz.model = "kimi-k2"
	m.renderModels()

	for _, it := range m.list.Items() {
		row, ok := it.(item)
		if ok && isSentinel(row.value) {
			t.Fatalf("picker list still contains an action row: %+v", row)
		}
	}
	idx := indexOfValue(m.list.Items(), "kimi-k2")
	m.list.Select(idx)

	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("updatePickModel returned %T, want model", next)
	}
	if got.view != viewReviewModels {
		t.Errorf("view after enter while editing = %v, want viewReviewModels", got.view)
	}
	nextForm, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	gotForm := nextForm.(model)
	if gotForm.view != viewEditForm {
		t.Errorf("view after confirm review = %v, want viewEditForm", gotForm.view)
	}
	if gotForm.wiz.model != "glm-4.6" {
		t.Errorf("model = %q, want the first checked id glm-4.6", gotForm.wiz.model)
	}
	if strings.Join(gotForm.wiz.models, ",") != "glm-4.6,kimi-k2" {
		t.Errorf("models = %v, want glm-4.6,kimi-k2 preserved", gotForm.wiz.models)
	}

	// A brand-new binding still needs a name; one model goes through the context
	// table shows the model and its derived window before naming the binding.
	fresh := pickerModel(t, []string{"kimi-k2", "glm-4.6"})
	fresh.toggleModel("glm-4.6")
	fresh.renderModels()
	fresh.list.Select(indexOfValue(fresh.list.Items(), "glm-4.6"))
	next, _ = fresh.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok = next.(model)
	if !ok {
		t.Fatalf("updatePickModel returned %T, want model", next)
	}
	if got.view != viewReviewModels {
		t.Errorf("view after enter on a new binding = %v, want viewReviewModels", got.view)
	}
	nextName, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	gotName := nextName.(model)
	if gotName.view != viewAddName {
		t.Errorf("view after confirming models = %v, want viewAddName", gotName.view)
	}
	if gotName.wiz.model != "glm-4.6" {
		t.Errorf("model = %q, want the first checked id glm-4.6", gotName.wiz.model)
	}
}

func TestPickerLandingRules(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6", "deepseek-v3"})

	// Rule 1: when nothing is chosen, land on the first available model.
	m.wiz.model = ""
	m.wiz.models = nil
	m.renderModels()
	if got := m.landingValue(); got != "kimi-k2" {
		t.Errorf("landingValue() = %q, want kimi-k2", got)
	}
	if sel, ok := m.list.SelectedItem().(item); !ok || sel.value != "kimi-k2" {
		t.Errorf("selected item = %+v, want first model", m.list.SelectedItem())
	}

	// Rule 2: a checked list lands on its first id, ahead of a stored model.
	m.toggleModel("glm-4.6")
	m.wiz.model = "deepseek-v3"
	m.renderModels()
	if got := m.landingValue(); got != "glm-4.6" {
		t.Errorf("landingValue() = %q, want the first checked id glm-4.6", got)
	}
	if sel, ok := m.list.SelectedItem().(item); !ok || sel.value != "glm-4.6" {
		t.Errorf("selected item = %+v, want glm-4.6", m.list.SelectedItem())
	}

	// Rule 3: with nothing checked, land on the stored model.
	m.wiz.models = nil
	m.wiz.model = "deepseek-v3"
	m.renderModels()
	if got := m.landingValue(); got != "deepseek-v3" {
		t.Errorf("landingValue() = %q, want %q", got, "deepseek-v3")
	}
	if sel, ok := m.list.SelectedItem().(item); !ok || sel.value != "deepseek-v3" {
		t.Errorf("selected item = %+v, want deepseek-v3 row", m.list.SelectedItem())
	}

	// Rule 4: when filtering, selection stays on the first row (index 0).
	m.modelFilter = "kimi"
	m.renderModels()
	if idx := m.list.Index(); idx != 0 {
		t.Errorf("list.Index() during filter = %d, want 0", idx)
	}
}

func TestPickerHasNoSkipRow(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6"})
	m.renderModels()
	for _, it := range m.list.Items() {
		if row, ok := it.(item); ok && strings.Contains(row.title, "skip") {
			t.Fatalf("picker still offers a skip row: %+v", row)
		}
	}
}

func TestPickerEnterOnModelFinishesAndKeepsFirstChecked(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6", "deepseek-v3"})
	m.fromForm = true
	m.wiz.models = []string{"kimi-k2"}
	m.wiz.model = "kimi-k2"
	m.renderModels()

	idx := indexOfValue(m.list.Items(), "glm-4.6")
	m.list.Select(idx)
	m.toggleModel("glm-4.6")

	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("updatePickModel returned %T, want model", next)
	}
	if got.view != viewReviewModels {
		t.Errorf("view = %v, want viewReviewModels", got.view)
	}
	nextForm, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	gotForm := nextForm.(model)
	if gotForm.view != viewEditForm {
		t.Errorf("view = %v, want viewEditForm", gotForm.view)
	}
	if gotForm.wiz.model != "kimi-k2" {
		t.Errorf("model = %q, want the first checked id kimi-k2", gotForm.wiz.model)
	}
	if gotStr := strings.Join(gotForm.wiz.models, ","); gotStr != "kimi-k2,glm-4.6" {
		t.Errorf("models = %q, want glm-4.6 joined into [kimi-k2 glm-4.6]", gotStr)
	}
}

func TestEnterKeyFinishesPicker(t *testing.T) {
	// Editing an existing binding returns to the form via review.
	editing := pickerModel(t, []string{"kimi-k2", "glm-4.6"})
	editing.wiz.edit = true
	editing.wiz.models = []string{"glm-4.6", "kimi-k2"}
	editing.renderModels()
	editing.list.Select(indexOfValue(editing.list.Items(), "kimi-k2"))
	next, _ := editing.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.view != viewReviewModels {
		t.Errorf("edit: view = %v, want viewReviewModels", got.view)
	}
	nextForm, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	gotForm := nextForm.(model)
	if gotForm.view != viewEditForm {
		t.Errorf("edit: view = %v, want viewEditForm", gotForm.view)
	}
	if gotForm.wiz.model != "glm-4.6" {
		t.Errorf("edit: model = %q, want first checked glm-4.6", gotForm.wiz.model)
	}

	// A new binding with a fetched list advances to the name step via review.
	fetched := pickerModel(t, []string{"kimi-k2", "glm-4.6"})
	fetched.wiz.models = []string{"kimi-k2"}
	fetched.renderModels()
	fetched.list.Select(indexOfValue(fetched.list.Items(), "kimi-k2"))
	next, _ = fetched.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(model)
	if got.view != viewReviewModels {
		t.Errorf("fetched: view = %v, want viewReviewModels", got.view)
	}
	nextName, _ := got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	gotName := nextName.(model)
	if gotName.view != viewAddName {
		t.Errorf("fetched: view = %v, want viewAddName", gotName.view)
	}
	if gotName.wiz.model != "kimi-k2" {
		t.Errorf("fetched: model = %q, want kimi-k2", gotName.wiz.model)
	}

	// A new binding whose ids were typed (no fetched list) also advances to name via review.
	manual := pickerModel(t, nil)
	manual.wiz.models = []string{"glm-4.6", "kimi-k2"}
	manual.wiz.model = ""
	manual.renderModels()
	next, _ = manual.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(model)
	if got.view != viewReviewModels {
		t.Errorf("manual: view = %v, want viewReviewModels", got.view)
	}
	nextName, _ = got.updateReviewModels(tea.KeyMsg{Type: tea.KeyEnter})
	gotName = nextName.(model)
	if gotName.view != viewAddName {
		t.Errorf("manual: view = %v, want viewAddName", gotName.view)
	}
	if gotName.wiz.model != "glm-4.6" {
		t.Errorf("manual: model = %q, want first checked glm-4.6", gotName.wiz.model)
	}
}

func TestMKeyOpensModelList(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6"})
	m.wiz.models = []string{"glm-4.6", "kimi-k2"}
	m.wiz.model = "kimi-k2"

	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("updatePickModel returned %T, want model", next)
	}
	if got.view != viewReviewModels {
		t.Errorf("view = %v, want viewReviewModels", got.view)
	}
	if strings.Join(got.wiz.models, ",") != "glm-4.6,kimi-k2" {
		t.Errorf("models = %v, want the existing selection kept", got.wiz.models)
	}
	if got.reviewCursor != len(got.wiz.models) {
		t.Errorf("cursor = %d, want the add row", got.reviewCursor)
	}
}

func TestPickerCtrlATogglesAllModels(t *testing.T) {
	all := []string{"kimi-k2", "glm-4.6", "deepseek-v3"}
	m := pickerModel(t, all)

	// 1. None selected -> Ctrl+A selects all models.
	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	got := next.(model)
	if strings.Join(got.wiz.models, ",") != strings.Join(all, ",") {
		t.Errorf("models after ctrl+a = %v, want all models %v", got.wiz.models, all)
	}

	// 2. All selected -> Ctrl+A clears the selection.
	next, _ = got.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	got = next.(model)
	if len(got.wiz.models) != 0 {
		t.Errorf("models after second ctrl+a = %v, want empty", got.wiz.models)
	}

	// 3. Partially selected -> Ctrl+A selects all remaining models.
	got.wiz.models = []string{"glm-4.6"}
	got.renderModels()
	next, _ = got.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	got = next.(model)
	if len(got.wiz.models) != len(all) {
		t.Errorf("models after partial ctrl+a = %v, want all %d models", got.wiz.models, len(all))
	}
	for _, id := range all {
		if !got.modelSelected(id) {
			t.Errorf("model %q missing from selection %v", id, got.wiz.models)
		}
	}

	// 4. Filter active -> Ctrl+A only toggles matching models.
	got.wiz.models = []string{"deepseek-v3"}
	got.modelFilter = "k" // matches kimi-k2 and deepseek-v3
	got.renderModels()

	// Both kimi-k2 and deepseek-v3 match. Only deepseek-v3 is selected, so ctrl+a should select kimi-k2.
	next, _ = got.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	got = next.(model)
	if !got.modelSelected("kimi-k2") || !got.modelSelected("deepseek-v3") {
		t.Errorf("models after filtered ctrl+a = %v, want both matching models selected", got.wiz.models)
	}

	// Now all matches are selected. Pressing Ctrl+A should deselect the matching subset.
	next, _ = got.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	got = next.(model)
	if got.modelSelected("kimi-k2") || got.modelSelected("deepseek-v3") {
		t.Errorf("models after second filtered ctrl+a = %v, want matching models deselected", got.wiz.models)
	}
}

func TestPickerCtrlARejectedForToolWithoutModelMenu(t *testing.T) {
	m := pickerModel(t, []string{"gpt-5.5", "gpt-5.4"})
	m.tool = &tools.Tool{Title: "Codex", ModelMenu: ""}
	m.renderModels()
	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyCtrlA})
	got := next.(model)
	if len(got.wiz.models) != 0 {
		t.Errorf("models = %v, want no curation for tool without model menu", got.wiz.models)
	}
	if !strings.Contains(got.status, "can't be given a model list") {
		t.Errorf("status = %q, want explanation that tool can't be given model list", got.status)
	}
}

// TestChromePinnedToTheBottom pins the layout rule every screen shares: the key legend
// is the last row of the terminal, the status line sits directly above it, and the
// legend stays on that row when a message appears — reserving the status row is what
// keeps the chrome from moving between screens and between states.
func TestChromePinnedToTheBottom(t *testing.T) {
	st := openTestCatalog(t, false)
	cases := []struct {
		name   string
		view   view
		setup  func(*model)
		legend string
	}{
		{"tools", viewTools, nil, "enter open • q/esc quit"},
		{"bindings", viewProfiles, func(m *model) { m.loadProfiles("") }, "enter switch • a add • e edit • v details • d delete"},
		{"form", viewEditForm, func(m *model) { m.loadEditForm() }, "↑/↓/j/k move • tab next field • enter select • s save • esc cancel"},
		{"input step", viewAddEndpoint, nil, "enter continue • esc back"},
		{"picker", viewPickModel, func(m *model) { m.showModels([]string{"glm-4.6", "kimi-k2"}) }, "enter finish • esc back"},
		{"fetching", viewFetching, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(st, "v1.0.0")
			m.width, m.height = 100, 30
			m.tool = m.findTool("claude")
			m.view = tc.view
			if tc.view == viewTools {
				m.loadTools()
			}
			if tc.setup != nil {
				tc.setup(&m)
			}
			m.resize()

			lines := strings.Split(m.View(), "\n")
			if len(lines) != m.height {
				t.Fatalf("view is %d rows, want %d — the chrome occupies the bottom", len(lines), m.height)
			}
			if tc.legend != "" && !strings.Contains(lines[len(lines)-1], tc.legend) {
				t.Errorf("last row = %q, want the legend %q", lines[len(lines)-1], tc.legend)
			}

			m.setStatus(statusOK, "saved")
			lines = strings.Split(m.View(), "\n")
			if len(lines) != m.height {
				t.Fatalf("view with a status is %d rows, want %d", len(lines), m.height)
			}
			if !strings.Contains(lines[len(lines)-2], "saved") {
				t.Errorf("status row = %q, want it directly above the legend", lines[len(lines)-2])
			}
			if tc.legend != "" && !strings.Contains(lines[len(lines)-1], tc.legend) {
				t.Errorf("a status message moved the legend: last row = %q", lines[len(lines)-1])
			}
		})
	}
}

// TestPickerFooterFollowsTheFilter: esc means "clear the search" while a query is
// active, so the legend has to say that instead of promising a way out.
func TestPickerFooterFollowsTheFilter(t *testing.T) {
	m := pickerModel(t, []string{"glm-4.6", "kimi-k2"})
	m.renderModels()
	if got := lastRow(m.View()); !strings.Contains(got, "esc back") {
		t.Errorf("unfiltered legend = %q, want esc back", got)
	}

	m.modelFilter = "glm"
	m.renderModels()
	if got := lastRow(m.View()); !strings.Contains(got, "esc clear filter") {
		t.Errorf("filtered legend = %q, want esc clear filter", got)
	}
}

func lastRow(view string) string {
	lines := strings.Split(view, "\n")
	return lines[len(lines)-1]
}

func TestDeleteActiveBindingRejectsInlineWithStatusErr(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://api.anthropic.com")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := st.PutCredential(p.ID, "sk-test-123456")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "claude-sonnet-4-5"); err != nil {
		t.Fatal(err)
	}
	bed, err := st.AddBinding("claude", "alpha", cr.ID, "claude-sonnet-4-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Activate(bed.ID); err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.width, m.height = 100, 30
	m.resize()
	m.tool = tools.Find("claude")
	m.view = viewProfiles
	m.loadProfiles("alpha")

	// Pressing 'd' on active binding must reject inline with statusErr without modal interruption or bell.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	mm := next.(model)
	if mm.showConfirm {
		t.Errorf("expected showConfirm to be false (non-modal feedback)")
	}
	if cmd != nil {
		t.Errorf("expected no cmd (no bell) on rejected action, got %v", cmd)
	}
	if mm.statusLvl != statusErr {
		t.Errorf("statusLvl = %v, want statusErr", mm.statusLvl)
	}
	if !strings.Contains(mm.status, `cannot delete active binding "alpha"`) {
		t.Errorf("status = %q, want cannot delete active binding...", mm.status)
	}
	view := mm.View()
	if !strings.Contains(view, "cannot delete active binding") {
		t.Errorf("view missing status error message:\n%s", view)
	}

	// Navigation remains immediately unblocked: pressing up/down moves cursor without needing esc/enter.
	next, _ = mm.Update(tea.KeyMsg{Type: tea.KeyUp})
	mm = next.(model)
	if mm.showConfirm {
		t.Errorf("navigation should not be blocked by modal")
	}
}

func TestDeleteInactiveBindingFlow(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://api.anthropic.com")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := st.PutCredential(p.ID, "sk-test-123456")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "claude-sonnet-4-5"); err != nil {
		t.Fatal(err)
	}
	bed, err := st.AddBinding("claude", "alpha", cr.ID, "claude-sonnet-4-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.AddBinding("claude", "beta", cr.ID, "claude-sonnet-4-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Activate(bed.ID); err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.width, m.height = 100, 30
	m.resize()
	m.tool = tools.Find("claude")
	m.view = viewProfiles
	m.loadProfiles("beta")

	// Pressing 'd' on inactive binding opens normal confirmation dialog.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	mm := next.(model)
	if !mm.showConfirm {
		t.Errorf("showConfirm = %v; want true", mm.showConfirm)
	}
	view := mm.View()
	if !strings.Contains(view, "Delete binding beta?") {
		t.Errorf("view missing 'Delete binding beta?':\n%s", view)
	}

	// Pressing enter confirms deletion.
	next, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm = next.(model)
	if mm.showConfirm {
		t.Errorf("expected showConfirm to be false after deletion")
	}
	if mm.statusLvl != statusOK {
		t.Errorf("statusLvl = %v, want statusOK", mm.statusLvl)
	}
	if !strings.Contains(mm.status, "Deleted beta") {
		t.Errorf("status = %q, want 'Deleted beta'", mm.status)
	}
	bs, _ := st.Bindings("claude")
	if len(bs) != 1 || bs[0].Name != "alpha" {
		t.Errorf("bindings after delete = %+v, want only alpha", bs)
	}
}

func TestUncheckingModelsAndDefaultShiftLogic(t *testing.T) {
	st := openTestCatalog(t, false)
	endpoint := "https://gateway.example.com/v1"
	p, err := st.PutProvider(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-populate catalog with a custom private model and a standard model
	if _, err := st.PutModelWithWindow(p.ID, "custom-private", 1_000_000); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "remote-model-1"); err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "v1.0.0")
	m.tool = tools.Find("opencode")
	m.view = viewPickModel
	m.wiz = wizard{
		endpoint: endpoint,
		key:      "sk-test",
		models:   []string{"custom-private", "remote-model-1"},
		model:    "custom-private",
	}

	// 1. Remote fetch returns new remote models without the private one
	remoteList := []models.Info{{ID: "remote-model-1"}, {ID: "remote-model-2"}}
	m.showModelInfos(remoteList)

	// Verify custom-private is preserved and still checked
	if !m.modelSelected("custom-private") {
		t.Errorf("custom-private was dropped from selection: %v", m.wiz.models)
	}
	foundInList := false
	for _, it := range m.list.Items() {
		if row, ok := it.(item); ok && row.value == "custom-private" {
			foundInList = true
			break
		}
	}
	if !foundInList {
		t.Errorf("custom-private not displayed in picker: %v", m.allModels)
	}

	// 2. Unchecking the default model (custom-private) shifts default to remaining checked (remote-model-1)
	m.toggleModel("custom-private")
	if m.wiz.model != "remote-model-1" {
		t.Errorf("default model = %q, want shifted to remote-model-1", m.wiz.model)
	}
	if !strings.Contains(m.status, "default model switched to remote-model-1") {
		t.Errorf("status missing switch notification: %q", m.status)
	}
	// Verify catalog metadata for custom-private is NOT deleted
	storedMods, err := st.ModelsForProvider(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var customMod *catalog.Model
	for i := range storedMods {
		if storedMods[i].Slug == "custom-private" {
			customMod = &storedMods[i]
			break
		}
	}
	if customMod == nil || customMod.ContextWindow != 1_000_000 {
		t.Errorf("catalog metadata was modified or deleted: customMod=%+v", customMod)
	}

	// 3. Unchecking all models and attempting enter -> blocked with error
	m.toggleModel("remote-model-1")
	if len(m.wiz.models) != 0 {
		t.Fatalf("expected 0 models checked, got %v", m.wiz.models)
	}
	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if got.view != viewPickModel {
		t.Errorf("expected viewPickModel on zero models enter, got %v", got.view)
	}
	if !strings.Contains(got.status, "at least 1 model must be selected") {
		t.Errorf("expected error status for 0 models, got %q", got.status)
	}

	// 4. Checking remote-model-2 and confirming enter transitions to reviewTable
	got.toggleModel("remote-model-2")
	nextRev, _ := got.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	revGot := nextRev.(model)
	if revGot.view != viewReviewModels {
		t.Fatalf("expected viewReviewModels, got %v", revGot.view)
	}
	if revGot.reviewSource != viewPickModel {
		t.Errorf("expected reviewSource viewPickModel, got %v", revGot.reviewSource)
	}

	// 5. Esc in reviewTable returns to viewPickModel with selection intact
	nextEsc, _ := revGot.updateReviewModels(tea.KeyMsg{Type: tea.KeyEsc})
	escGot := nextEsc.(model)
	if escGot.view != viewPickModel {
		t.Errorf("expected return to viewPickModel on Esc, got %v", escGot.view)
	}
	if !escGot.modelSelected("remote-model-2") {
		t.Errorf("remote-model-2 should still be checked after Esc")
	}
}

func TestEnterOnActiveBindingShowsAlreadyActive(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://api.anthropic.com")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := st.PutCredential(p.ID, "sk-test-123456")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "claude-sonnet-4-5"); err != nil {
		t.Fatal(err)
	}
	b1, err := st.AddBinding("claude", "primary", cr.ID, "claude-sonnet-4-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.AddBinding("claude", "secondary", cr.ID, "claude-sonnet-4-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Activate(b1.ID); err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.width, m.height = 100, 30
	m.resize()
	m.tool = tools.Find("claude")
	m.view = viewProfiles
	m.loadProfiles("primary")

	// 1. Enter on currently active binding displays "already active" info notice
	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := res.(model)
	if updated.view != viewProfiles {
		t.Errorf("view = %v, want viewProfiles", updated.view)
	}
	if updated.statusLvl != statusInfo {
		t.Errorf("statusLvl = %v, want statusInfo", updated.statusLvl)
	}
	if updated.status != "primary is already active" {
		t.Errorf("status = %q, want %q", updated.status, "primary is already active")
	}

	// 2. Enter on inactive binding switches successfully
	updated.loadProfiles("secondary")
	res2, _ := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	switched := res2.(model)
	if switched.statusLvl != statusOK {
		t.Errorf("statusLvl = %v, want statusOK", switched.statusLvl)
	}
	if !strings.HasPrefix(switched.status, "Switched to secondary") {
		t.Errorf("status = %q, want prefix 'Switched to secondary'", switched.status)
	}

	// 3. Enter again on now-active secondary displays "already active" notice
	res3, _ := switched.Update(tea.KeyMsg{Type: tea.KeyEnter})
	alreadyActive := res3.(model)
	if alreadyActive.statusLvl != statusInfo {
		t.Errorf("statusLvl = %v, want statusInfo", alreadyActive.statusLvl)
	}
	if alreadyActive.status != "secondary is already active" {
		t.Errorf("status = %q, want %q", alreadyActive.status, "secondary is already active")
	}

	rewriteModelJSON(t, filepath.Join(os.Getenv("HOME"), ".claude", "settings.json"), "claude-sonnet-4-5", "manual-model")
	if _, drift := alreadyActive.bindingRow("secondary"); !strings.Contains(drift, "saved:") || !strings.Contains(drift, "live:") || !strings.Contains(drift, "manual-model") {
		t.Fatalf("binding row does not distinguish saved and live config: %q", drift)
	}
	alreadyActive.loadProfiles("secondary")
	sawDriftRow, driftValue := false, ""
	for _, raw := range alreadyActive.list.Items() {
		it, ok := raw.(item)
		if !ok || !strings.HasPrefix(it.value, driftSentinel) {
			continue
		}
		sawDriftRow, driftValue = true, it.value
	}
	if !sawDriftRow {
		t.Fatal("drift did not render its own annotation row under the binding")
	}
	if !inertSentinel(driftValue) {
		t.Errorf("drift row %q must be inert so the cursor cannot rest on it", driftValue)
	}
	alreadyActive.loadTools()
	seenLive := false
	for _, raw := range alreadyActive.list.Items() {
		if it, ok := raw.(item); ok && it.value == "claude" {
			seenLive = strings.Contains(it.desc, "last confirmed: secondary") && strings.Contains(it.desc, "live:") && strings.Contains(it.desc, "manual-model")
		}
	}
	if !seenLive {
		t.Fatal("tool status does not separate last applied binding from live model")
	}
	alreadyActive.view = viewProfiles
	alreadyActive.loadProfiles("secondary")
	res4, _ := alreadyActive.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	reapplied := res4.(model)
	if reapplied.statusLvl != statusOK || reapplied.status != "Reapplied secondary" {
		t.Fatalf("reapply status = %q (%v)", reapplied.status, reapplied.statusLvl)
	}
	redata, rerr := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".claude", "settings.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	var after map[string]any
	if err := json.Unmarshal(redata, &after); err != nil || after["model"] != "claude-sonnet-4-5" {
		t.Fatalf("TUI reapply did not restore the saved default model: %v (err %v)", after["model"], err)
	}
}

func TestShowModelInfosDoesNotEagerlyPersist(t *testing.T) {
	st := openTestCatalog(t, false)
	endpoint := "https://gateway.example.com/v1"

	m := newModel(st, "test")
	m.tool = tools.Find("claude")
	m.view = viewPickModel
	m.wiz = wizard{
		endpoint: endpoint,
		key:      "sk-test",
	}

	remoteList := []models.Info{{ID: "remote-a"}, {ID: "remote-b"}}
	m.showModelInfos(remoteList)

	// In memory, models are populated
	if len(m.allModels) != 2 || m.allModels[0] != "remote-a" || m.allModels[1] != "remote-b" {
		t.Fatalf("allModels = %v, want [remote-a remote-b]", m.allModels)
	}

	// In database / catalog on disk, nothing should have been written yet
	ps, err := st.Providers()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 0 {
		t.Errorf("providers should not be written eagerly: %+v", ps)
	}
	ms, err := st.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Errorf("models should not be written eagerly: %+v", ms)
	}

	// Select remote-a and finish picker to advance to review
	m.toggleModel("remote-a")
	res, _ := m.finishPicker()
	m = res.(model)
	if m.view != viewReviewModels {
		t.Fatalf("view = %v, want viewReviewModels", m.view)
	}

	// Still nothing on disk
	ms, _ = st.Models()
	if len(ms) != 0 {
		t.Errorf("models should remain empty during review: %+v", ms)
	}

	// Now finalize add
	resAdd, _ := m.finishAdd("my-binding")
	m = resAdd.(model)
	if m.statusLvl == statusErr {
		t.Fatalf("finishAdd failed: %s", m.status)
	}

	// Now disk has only the chosen model (remote-a), NOT remote-b!
	ms, _ = st.Models()
	if len(ms) != 1 || ms[0].Slug != "remote-a" {
		t.Errorf("only chosen model should be saved to catalog, got: %+v", ms)
	}
}

func TestModelLibraryCursorRetention(t *testing.T) {
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://test.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	m1, err := st.PutModelWithWindow(p.ID, "mod-1", 100000)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := st.PutModelWithWindow(p.ID, "mod-2", 200000)
	if err != nil {
		t.Fatal(err)
	}
	m3, err := st.PutModelWithWindow(p.ID, "mod-3", 300000)
	if err != nil {
		t.Fatal(err)
	}

	m := newModel(st, "test")
	m.view = viewModels
	m.loadModels("")

	// Initially at index 1 (the column labels lead the table, mod-1 follows)
	if m.list.Index() != 1 {
		t.Errorf("initial index = %d, want 1", m.list.Index())
	}

	// Select index 2 (mod-2)
	m.list.Select(2)
	if it, ok := m.list.SelectedItem().(item); !ok || it.value != m2.ID {
		t.Fatalf("selected = %+v, want %s", it, m2.ID)
	}

	// Delete mod-2 with "d" key
	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	afterDel := res.(model)
	if afterDel.statusLvl == statusErr {
		t.Fatalf("delete failed: %s", afterDel.status)
	}

	// Cursor should remain at index 2 (which is now mod-3, NOT reset to 0!)
	if afterDel.list.Index() != 2 {
		t.Errorf("cursor after delete = %d, want 2", afterDel.list.Index())
	}
	if it, ok := afterDel.list.SelectedItem().(item); !ok || it.value != m3.ID {
		t.Errorf("item at index 2 after delete = %+v, want %s (mod-3)", it, m3.ID)
	}

	// Edit context window of mod-3
	resEdit, _ := afterDel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	inEdit := resEdit.(model)
	if inEdit.view != viewWindowPresets {
		t.Fatalf("view = %v, want viewWindowPresets", inEdit.view)
	}
	if !strings.Contains(inEdit.list.Title, "current 300,000") {
		t.Fatalf("preset title = %q, want the current window", inEdit.list.Title)
	}
	inEdit.list.Select(indexOfValue(inEdit.list.Items(), windowCustomSentinel))
	custom, _ := inEdit.updateWindowPresets(tea.KeyMsg{Type: tea.KeyEnter})
	inEdit = custom.(model)
	if inEdit.view != viewModelWindow {
		t.Fatalf("view = %v, want viewModelWindow after Custom…", inEdit.view)
	}
	inEdit.input.SetValue("350000")
	resSaved, _ := inEdit.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	afterSaved := resSaved.(model)
	if afterSaved.view != viewModels {
		t.Fatalf("view = %v, want viewModels", afterSaved.view)
	}

	// Cursor should land on mod-3 (index 2), NOT reset to 0!
	if afterSaved.list.Index() != 2 {
		t.Errorf("cursor after editing window = %d, want 2", afterSaved.list.Index())
	}
	if it, ok := afterSaved.list.SelectedItem().(item); !ok || it.value != m3.ID {
		t.Errorf("item after editing window = %+v, want %s (mod-3)", it, m3.ID)
	}

	_ = m1
}

// rewriteModelJSON sets settings' top-level model, JSON-aware, so the test
// does not depend on the file's formatting.
func rewriteModelJSON(t *testing.T, path, want, set string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings.json not valid JSON: %v", err)
	}
	if s["model"] != want {
		t.Fatalf("saved model missing from settings: model = %v, want %v", s["model"], want)
	}
	s["model"] = set
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// aggregationCatalog points a bare catalog at hand-written tables, so the hint
// can be produced without opening a store and running its migrations.
func aggregationCatalog(t *testing.T, providers, credentials, models, bindings string) *catalog.Catalog {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"providers.json":   providers,
		"credentials.json": credentials,
		"models.json":      models,
		"bindings.json":    bindings,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &catalog.Catalog{Root: dir}
}

func TestAggregationNote(t *testing.T) {
	// The hint counts the tool's own bindings: each one is an endpoint the tool's
	// config will register.
	const twoProviders = `[{"id":"p1","baseUrl":"https://one.example/v1"},{"id":"p2","baseUrl":"https://two.example/v1"}]`
	const twoCredentials = `[{"id":"k1","providerId":"p1","key":"sk-one"},{"id":"k2","providerId":"p2","key":"sk-two"}]`
	const threeModels = `[{"id":"m1","providerId":"p1","slug":"a"},{"id":"m2","providerId":"p2","slug":"b"},{"id":"m3","providerId":"p2","slug":"c"}]`
	const piBindings = `[{"id":"b1","name":"one","tool":"pi","credentialId":"k1","modelId":"m1","models":["m1"]},` +
		`{"id":"b2","name":"two","tool":"pi","credentialId":"k2","modelId":"m2","models":["m2","m3"]}]`

	twoEndpoints := func(t *testing.T) *catalog.Catalog {
		return aggregationCatalog(t, twoProviders, twoCredentials, threeModels, piBindings)
	}
	oneEndpoint := func(t *testing.T) *catalog.Catalog {
		return aggregationCatalog(t,
			`[{"id":"p1","baseUrl":"https://one.example/v1"}]`,
			`[{"id":"k1","providerId":"p1","key":"sk-one"}]`,
			`[{"id":"m1","providerId":"p1","slug":"a"}]`,
			`[{"id":"b1","name":"one","tool":"pi","credentialId":"k1","modelId":"m1","models":["m1"]}]`)
	}

	tests := []struct {
		name string
		tool *tools.Tool
		cat  func(*testing.T) *catalog.Catalog
		want string
	}{
		{name: "single-endpoint tool", tool: tools.Find("codex"), cat: twoEndpoints, want: ""},
		{name: "no tool", tool: nil, cat: twoEndpoints, want: ""},
		{name: "empty catalog", tool: tools.Find("pi"), cat: func(t *testing.T) *catalog.Catalog {
			return &catalog.Catalog{Root: t.TempDir()}
		}, want: ""},
		{name: "one endpoint", tool: tools.Find("pi"), cat: oneEndpoint, want: ""},
		{name: "only one endpoint has a key", tool: tools.Find("pi"), cat: func(t *testing.T) *catalog.Catalog {
			return aggregationCatalog(t, twoProviders,
				`[{"id":"k1","providerId":"p1","key":"sk-one"},{"id":"k2","providerId":"p2","key":""}]`,
				threeModels, piBindings)
		}, want: ""},
		{name: "bindings of another tool", tool: tools.Find("pi"), cat: func(t *testing.T) *catalog.Catalog {
			return aggregationCatalog(t, twoProviders, twoCredentials, threeModels,
				`[{"id":"b1","name":"one","tool":"codex","credentialId":"k1","modelId":"m1","models":["m1"]},`+
					`{"id":"b2","name":"two","tool":"codex","credentialId":"k2","modelId":"m2","models":["m2","m3"]}]`)
		}, want: ""},
		{name: "two endpoints", tool: tools.Find("pi"), cat: twoEndpoints, want: "/model: 2 endpoints (3 models)"},
		{name: "unreadable catalog", tool: tools.Find("pi"), cat: func(t *testing.T) *catalog.Catalog {
			return aggregationCatalog(t, "{", "[]", "[]", "[]")
		}, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregationNote(tc.tool, tc.cat(t)); got != tc.want {
				t.Fatalf("aggregationNote() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFormEscKeepsEditsThenDiscards pins the dirty guard: esc on a changed field
// leaves the field without losing the edit, and a second esc discards the form.
func TestFormEscKeepsEditsThenDiscards(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "test")
	m.tool = m.findTool("claude")
	m.width, m.height = 100, 30
	m.wiz = wizard{name: "relay", endpoint: "https://relay.example/v1", key: "sk-x",
		model: "claude-3", models: []string{"claude-3"}}
	m.view = viewEditForm
	m.loadEditForm()

	// Type into the endpoint field, as updateEditForm would.
	m.formInputs[focusURL].SetValue("https://other.example/v1")
	m.wiz.endpoint = "https://other.example/v1"

	next, _ := m.updateEditForm(tea.KeyMsg{Type: tea.KeyEsc})
	kept := next.(model)
	if kept.view != viewEditForm || !kept.discardArmed {
		t.Fatalf("first esc: view=%v armed=%v, want the form still open and armed", kept.view, kept.discardArmed)
	}
	if kept.formFocus != focusModels {
		t.Errorf("first esc: focus = %d, want the Models row", kept.formFocus)
	}
	if !strings.Contains(kept.status, "edits kept") {
		t.Errorf("status = %q, want the edits-kept warning", kept.status)
	}

	next, _ = kept.updateEditForm(tea.KeyMsg{Type: tea.KeyEsc})
	discarded := next.(model)
	if discarded.view != viewProfiles {
		t.Errorf("second esc: view = %v, want viewProfiles", discarded.view)
	}
	if discarded.discardArmed || discarded.formBaselineSet {
		t.Errorf("discard left guard state behind: armed=%v baseline=%v", discarded.discardArmed, discarded.formBaselineSet)
	}
}

// TestFormEscExitsWhenClean: nothing typed means esc is still one press.
func TestFormEscExitsWhenClean(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "test")
	m.tool = m.findTool("claude")
	m.wiz = wizard{name: "relay", endpoint: "https://relay.example/v1", key: "sk-x",
		model: "claude-3", models: []string{"claude-3"}}
	m.view = viewEditForm
	m.loadEditForm()

	next, _ := m.updateEditForm(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(model); got.view != viewProfiles {
		t.Errorf("clean esc: view = %v, want viewProfiles", got.view)
	}
}

// TestFormSaveKey: s on an action row saves; s in a text field is just a letter.
func TestFormSaveKey(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "test")
	m.tool = m.findTool("claude")
	m.width, m.height = 100, 30
	m.wiz = wizard{name: "relay", endpoint: "https://relay.example/v1", key: "sk-x",
		model: "claude-3", models: []string{"claude-3"}}
	m.view = viewEditForm
	m.loadEditForm()

	m.formInputs[focusName].SetValue("relay-s")
	m.formInputs[focusName].CursorEnd()
	m.wiz.name = "relay-s"
	next, _ := m.updateEditForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	typed := next.(model)
	if typed.view != viewEditForm || !strings.HasSuffix(typed.formInputs[focusName].Value(), "s") {
		t.Fatalf("s in a text field: view=%v value=%q, want it typed", typed.view, typed.formInputs[focusName].Value())
	}

	typed.formFocus = focusModels
	typed.applyFormFocus()
	next, _ = typed.updateEditForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	saved := next.(model)
	if saved.view != viewProfiles {
		t.Fatalf("s on an action row: view = %v, want viewProfiles; status=%q", saved.view, saved.status)
	}
	if _, found, err := st.BindingByName("claude", "relay-ss"); err != nil || !found {
		bindings, _ := st.Bindings("claude")
		names := make([]string, len(bindings))
		for i, b := range bindings {
			names[i] = b.Name
		}
		t.Fatalf("binding not saved: found=%v err=%v names=%v", found, err, names)
	}
}

// TestReviewFetchKey: f starts a fetch from the model list and remembers to
// return there, so confirming the picker does not dump the user back to the form.
func TestReviewFetchKey(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x"}
	m.view = viewReviewModels
	m.reviewCursor = 0

	next, cmd := m.updateReviewModels(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	got := next.(model)
	if got.view != viewFetching || got.fetchReturn != viewReviewModels {
		t.Fatalf("view=%v return=%v, want fetching with review return", got.view, got.fetchReturn)
	}
	if cmd == nil {
		t.Fatal("fetch did not return a command")
	}
}

// TestApplyFetchedFailureStaysInReview: a failed fetch from the list leaves the
// cursor and the list exactly where they were.
func TestApplyFetchedFailureStaysInReview(t *testing.T) {
	m := pickerModel(t, nil)
	m.view = viewFetching
	m.fetchReturn = viewReviewModels
	m.reviewCursor = 3

	next, _ := m.applyFetched(fetchedMsg{err: errors.New("no /v1/models")})
	got := next.(model)
	if got.view != viewReviewModels || got.reviewCursor != 3 {
		t.Fatalf("view=%v cursor=%d, want the previous review state", got.view, got.reviewCursor)
	}
	if got.statusLvl != statusErr {
		t.Errorf("status level = %v, want error", got.statusLvl)
	}
}

// TestPickerEscReturnsToReviewAfterFetch: esc backs out one level, to the list
// the fetch started from.
func TestPickerEscReturnsToReviewAfterFetch(t *testing.T) {
	m := pickerModel(t, []string{"glm-4.6"})
	m.fetchReturn = viewReviewModels
	m.view = viewPickModel

	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(model); got.view != viewReviewModels {
		t.Errorf("view = %v, want viewReviewModels", got.view)
	}
}

// TestHelpOverlay: ? opens a sheet with the current keys and any key closes it.
func TestHelpOverlay(t *testing.T) {
	st := openTestCatalog(t, false)
	m := newModel(st, "test")
	m.width, m.height = 100, 30
	m.resize()
	m.loadTools()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	opened := next.(model)
	if !opened.showHelp {
		t.Fatal("? did not open the help sheet")
	}
	view := opened.View()
	for _, want := range []string{"Keyboard shortcuts", "enter", "ctrl+c"} {
		if !strings.Contains(view, want) {
			t.Errorf("help sheet missing %q", want)
		}
	}

	next, _ = opened.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if closed := next.(model); closed.showHelp {
		t.Error("any key should close the help sheet")
	}
}

// TestHelpKeepsQuestionMarkAsInput: a focused text field owns the key.
func TestHelpKeepsQuestionMarkAsInput(t *testing.T) {
	m := newModel(openTestCatalog(t, false), "test")
	m.view = viewAddName
	m.startInput("name", false)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	got := next.(model)
	if got.showHelp {
		t.Fatal("? must type in a text field, not open help")
	}
	if !strings.Contains(got.input.Value(), "?") {
		t.Fatalf("input = %q, want the typed ?", got.input.Value())
	}
}

// TestFormDirtyTracksSelectedModelsAndContext: fetched windows for unselected
// models are staging data, not edits; selected models and their windows are.
func TestFormDirtyTracksSelectedModelsAndContext(t *testing.T) {
	w := wizard{name: "x", endpoint: "https://e/v1", key: "k", model: "a", models: []string{"a"},
		windows: map[string]int{"a": 1000}}
	m := model{wiz: w, formBaseline: captureForm(w), formBaselineSet: true}
	if m.formDirty() {
		t.Fatal("clean form reported dirty")
	}
	m.wiz.windows["b"] = 2000 // an unselected fetched model
	if m.formDirty() {
		t.Fatal("unselected window marked the form dirty")
	}
	m.wiz.windows["a"] = 2000
	if !m.formDirty() {
		t.Fatal("selected window change not detected")
	}
	m.wiz.windows["a"] = 1000
	m.wiz.models = append(m.wiz.models, "b")
	if !m.formDirty() {
		t.Fatal("model list change not detected")
	}
}

// bindingDetailModel builds a catalog with one active binding named relay and
// returns the model positioned on the bindings screen with that row selected.
func bindingDetailModel(t *testing.T, width, height int) model {
	t.Helper()
	return bindingDetailModelNamed(t, width, height, "relay")
}

// bindingDetailModelNamed is bindingDetailModel with a caller-chosen binding
// name, so tests can exercise long or wide names.
func bindingDetailModelNamed(t *testing.T, width, height int, name string) model {
	t.Helper()
	st := openTestCatalog(t, false)
	p, err := st.PutProvider("https://gateway.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	cr, err := st.PutCredential(p.ID, "sk-test-123456")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModelWithWindow(p.ID, "glm-4.6", 200000); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutModel(p.ID, "kimi-k2"); err != nil {
		t.Fatal(err)
	}
	b, err := st.AddBinding("claude", name, cr.ID, "glm-4.6", []string{"glm-4.6", "kimi-k2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Activate(b.ID); err != nil {
		t.Fatal(err)
	}
	m := newModel(st, "test")
	m.tool = m.findTool("claude")
	m.width, m.height = width, height
	m.view = viewProfiles
	m.loadProfiles(name)
	return m
}

// TestBindingDetailShowsFullBinding: the wide pane exposes what the table has to
// truncate — the masked key, every model and its context source, and the status.
func TestBindingDetailShowsFullBinding(t *testing.T) {
	m := bindingDetailModel(t, 120, 30)
	if !m.wideDetail() {
		t.Fatal("width 120 should use the wide detail layout")
	}
	if got := m.list.Width(); got != m.detailListWidth() {
		t.Errorf("list width = %d, want %d", got, m.detailListWidth())
	}
	view := m.View()
	for _, want := range []string{"API key", "Default", "kimi-k2", "200,000", "manual", "sk-tes••••3456", "v details"} {
		if !strings.Contains(view, want) {
			t.Errorf("wide detail missing %q", want)
		}
	}
	if strings.Contains(view, "sk-test-123456") {
		t.Fatal("wide detail exposed the raw API key")
	}
	if strings.Contains(view, "* default") {
		t.Fatal("detail should state the default once, not mark rows")
	}
	if rows := len(strings.Split(view, "\n")); rows != m.height {
		t.Fatalf("wide view has %d rows, want %d", rows, m.height)
	}
}

// TestBindingDetailPaneTopPadding: the detail pane starts detailTopPad blank
// rows down, aligning its title with the table header and its values with the
// first binding row.
func TestBindingDetailPaneTopPadding(t *testing.T) {
	m := bindingDetailModel(t, 120, 30)
	lines := strings.Split(m.renderBindingDetailPane(), "\n")
	if len(lines) <= detailTopPad {
		t.Fatalf("detail pane has %d lines, want more than %d", len(lines), detailTopPad)
	}
	for i := 0; i < detailTopPad; i++ {
		if got := strings.TrimSpace(lines[i]); got != "" {
			t.Errorf("line %d = %q, want blank", i, got)
		}
	}
	if got := strings.TrimSpace(lines[detailTopPad]); got != "relay" {
		t.Errorf("line %d = %q, want %q", detailTopPad, got, "relay")
	}
}

// TestBindingDetailNarrowOpensWithV: narrow terminals keep the list until v opens
// the same detail; esc returns and e opens the existing form.
func TestBindingDetailNarrowOpensWithV(t *testing.T) {
	m := bindingDetailModel(t, 80, 30)
	if m.wideDetail() {
		t.Fatal("width 80 must not use the wide layout")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	detail := next.(model)
	if detail.view != viewBindingDetail {
		t.Fatalf("v: view = %v, want viewBindingDetail", detail.view)
	}
	if view := detail.View(); !strings.Contains(view, "relay") || !strings.Contains(view, "API key") {
		t.Fatal("detail screen missing the binding values")
	}

	next, _ = detail.Update(tea.KeyMsg{Type: tea.KeyEsc})
	back := next.(model)
	if back.view != viewProfiles {
		t.Fatalf("esc: view = %v, want viewProfiles", back.view)
	}

	next, _ = back.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	detail = next.(model)
	next, _ = detail.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	edit := next.(model)
	if edit.view != viewEditForm {
		t.Fatalf("e: view = %v, want viewEditForm", edit.view)
	}
	if edit.wiz.origName != "relay" {
		t.Errorf("edit target = %q, want relay", edit.wiz.origName)
	}
}

func TestWideDetailThreshold(t *testing.T) {
	m := model{view: viewProfiles}
	m.width = wideDetailMinWidth - 1
	if m.wideDetail() {
		t.Errorf("width %d should be narrow", m.width)
	}
	m.width = wideDetailMinWidth
	if !m.wideDetail() {
		t.Errorf("width %d should be wide", m.width)
	}
	m.view = viewBindingDetail
	if m.wideDetail() {
		t.Error("the full-screen detail is not the wide list layout")
	}
}

// bindingHeaderCells returns the bindings table's column labels.
func bindingHeaderCells(t *testing.T, m model) []string {
	t.Helper()
	for _, raw := range m.list.Items() {
		if it, ok := raw.(item); ok && it.value == hdrSentinel {
			return it.cells
		}
	}
	t.Fatal("bindings table header not found")
	return nil
}

// bindingDataCellCounts returns the cell count of every binding row; sentinel
// rows (header, divider, add, drift annotations) are skipped.
func bindingDataCellCounts(t *testing.T, m model) []int {
	t.Helper()
	var counts []int
	for _, raw := range m.list.Items() {
		it, ok := raw.(item)
		if !ok || isSentinel(it.value) || it.cells == nil {
			continue
		}
		counts = append(counts, len(it.cells))
	}
	return counts
}

// TestWideBindingsTableKeepsOnlyNameColumn: the wide detail pane already shows
// the endpoint and models, so the list beside it drops those columns; the
// narrow table keeps them because it has no pane.
func TestWideBindingsTableKeepsOnlyNameColumn(t *testing.T) {
	m := bindingDetailModel(t, 120, 30)
	if header := bindingHeaderCells(t, m); len(header) != 1 || header[0] != "name" {
		t.Fatalf("wide header = %v, want [name]", header)
	}
	for _, n := range bindingDataCellCounts(t, m) {
		if n != 1 {
			t.Errorf("wide binding row has %d cells, want 1", n)
		}
	}
	listView := m.list.View()
	for _, label := range []string{"endpoint", "model", "more"} {
		if strings.Contains(listView, label) {
			t.Errorf("wide list still shows the %q column", label)
		}
	}
	view := m.View()
	for _, want := range []string{"Endpoint", "https://gateway.example/v1", "Default"} {
		if !strings.Contains(view, want) {
			t.Errorf("wide view missing %q from the detail pane", want)
		}
	}
}

// TestNarrowBindingsTableKeepsAllColumns keeps the narrow layout as it was:
// endpoint, model, and more still label the full-width list.
func TestNarrowBindingsTableKeepsAllColumns(t *testing.T) {
	m := bindingDetailModel(t, 80, 30)
	if header := bindingHeaderCells(t, m); strings.Join(header, ",") != "name,endpoint,model,more" {
		t.Fatalf("narrow header = %v, want the four column labels", header)
	}
	for _, n := range bindingDataCellCounts(t, m) {
		if n != 4 {
			t.Errorf("narrow binding row has %d cells, want 4", n)
		}
	}
	listView := m.list.View()
	for _, label := range []string{"endpoint", "model", "more"} {
		if !strings.Contains(listView, label) {
			t.Errorf("narrow list missing the %q column label", label)
		}
	}
}

// TestWindowResizeRebuildsBindingColumns: crossing the wide/narrow threshold
// rebuilds the rows for the new layout and keeps the cursor on the same binding.
func TestWindowResizeRebuildsBindingColumns(t *testing.T) {
	m := bindingDetailModel(t, 80, 30)
	if got, ok := m.highlightedBindingName(); !ok || got != "relay" {
		t.Fatalf("initial highlight = %q, %v; want relay", got, ok)
	}

	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	wide := next.(model)
	if !wide.wideDetail() {
		t.Fatal("width 120 should use the wide layout")
	}
	if header := bindingHeaderCells(t, wide); len(header) != 1 || header[0] != "name" {
		t.Fatalf("after widening: header = %v, want [name]", header)
	}
	if got, ok := wide.highlightedBindingName(); !ok || got != "relay" {
		t.Fatalf("after widening: highlight = %q, %v; want relay", got, ok)
	}

	next, _ = wide.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	narrow := next.(model)
	if narrow.wideDetail() {
		t.Fatal("width 80 should use the narrow layout")
	}
	if header := bindingHeaderCells(t, narrow); strings.Join(header, ",") != "name,endpoint,model,more" {
		t.Fatalf("after narrowing: header = %v, want the four column labels", header)
	}
	if got, ok := narrow.highlightedBindingName(); !ok || got != "relay" {
		t.Fatalf("after narrowing: highlight = %q, %v; want relay", got, ok)
	}
}

// TestEscFromBindingDetailRebuildsBindingColumns: resizing while the
// full-screen detail is open can cross the wide/narrow threshold; esc must
// rebuild the rows for the width it returns to instead of reusing stale ones.
func TestEscFromBindingDetailRebuildsBindingColumns(t *testing.T) {
	tests := []struct {
		name          string
		fromWidth     int
		toWidth       int
		wantHeader    []string
		wantCellCount int
	}{
		{
			name:          "narrow to wide",
			fromWidth:     80,
			toWidth:       120,
			wantHeader:    []string{"name"},
			wantCellCount: 1,
		},
		{
			name:          "wide to narrow",
			fromWidth:     120,
			toWidth:       80,
			wantHeader:    []string{"name", "endpoint", "model", "more"},
			wantCellCount: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := bindingDetailModel(t, tt.fromWidth, 30)
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
			detail := next.(model)
			if detail.view != viewBindingDetail {
				t.Fatalf("v: view = %v, want viewBindingDetail", detail.view)
			}

			next, _ = detail.Update(tea.WindowSizeMsg{Width: tt.toWidth, Height: 30})
			detail = next.(model)
			if detail.view != viewBindingDetail {
				t.Fatalf("resize: view = %v, want viewBindingDetail", detail.view)
			}

			next, _ = detail.Update(tea.KeyMsg{Type: tea.KeyEsc})
			back := next.(model)
			if back.view != viewProfiles {
				t.Fatalf("esc: view = %v, want viewProfiles", back.view)
			}
			if header := bindingHeaderCells(t, back); strings.Join(header, ",") != strings.Join(tt.wantHeader, ",") {
				t.Fatalf("esc: header = %v, want %v", header, tt.wantHeader)
			}
			counts := bindingDataCellCounts(t, back)
			if len(counts) != 1 || counts[0] != tt.wantCellCount {
				t.Fatalf("esc: binding cells = %v, want one row with %d cells", counts, tt.wantCellCount)
			}
			if got, ok := back.highlightedBindingName(); !ok || got != "relay" {
				t.Fatalf("esc: highlight = %q, %v; want relay", got, ok)
			}
		})
	}
}

func TestHardWrapDetailValue(t *testing.T) {
	if got := strings.Join(hardWrap("abcdef", 3), "|"); got != "abc|def" {
		t.Errorf("hardWrap = %q, want abc|def", got)
	}
	if got := hardWrap("ab", 3); len(got) != 1 || got[0] != "ab" {
		t.Errorf("hardWrap short value = %q, want one chunk", got)
	}
	if got := strings.Join(hardWrap("one two three", 7), "|"); got != "one two|three" {
		t.Errorf("hardWrap at a space = %q, want one two|three", got)
	}
}

// TestHardWrapWideRunes: CJK runes occupy two columns, so wrapping must count
// display width rather than runes.
func TestHardWrapWideRunes(t *testing.T) {
	got := hardWrap("中文名字", 4)
	if strings.Join(got, "|") != "中文|名字" {
		t.Fatalf("hardWrap = %q, want 中文|名字", got)
	}
	for _, part := range got {
		if w := lipgloss.Width(part); w > 4 {
			t.Errorf("chunk %q width = %d, want <= 4", part, w)
		}
	}
}

// TestBindingDetailLongNameWraps: a 60-character binding name must wrap inside
// the detail pane instead of widening the row past the terminal.
func TestBindingDetailLongNameWraps(t *testing.T) {
	name := strings.Repeat("x", 60)
	m := bindingDetailModelNamed(t, 110, 30, name)
	if !m.wideDetail() {
		t.Fatal("width 110 should use the wide detail layout")
	}
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("view has %d rows, want %d", len(lines), m.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, w, m.width, line)
		}
	}
	if strings.Contains(view, name) {
		t.Fatal("60-character name should wrap, not render on one line")
	}
	width := m.detailPaneWidth() - detailGap
	if width < 20 {
		width = 20
	}
	parts := hardWrap(name, width)
	if len(parts) < 2 {
		t.Fatalf("name wrapped into %d chunks, want multiple", len(parts))
	}
	for _, part := range parts {
		if !strings.Contains(view, part) {
			t.Errorf("detail missing wrapped name chunk %q", part)
		}
	}
}

// TestBindingDetailInactiveNote: only tools that write a single endpoint need the
// "switch to write" hint. Pi registers every visible binding's endpoint in its
// provider list, so an inactive binding is already on disk.
func TestBindingDetailInactiveNote(t *testing.T) {
	for _, tc := range []struct {
		tool       string
		wantNote   bool
		wantStatus string
		reject     string
	}{
		{"claude", true, "inactive", "registered · not default"},
		{"pi", false, "registered · not default", "inactive"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			st := openTestCatalog(t, false)
			p, err := st.PutProvider("https://gateway.example/v1")
			if err != nil {
				t.Fatal(err)
			}
			cr, err := st.PutCredential(p.ID, "sk-test-123456")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.PutModel(p.ID, "test-model"); err != nil {
				t.Fatal(err)
			}
			active, err := st.AddBinding(tc.tool, "active", cr.ID, "test-model", []string{"test-model"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.AddBinding(tc.tool, "other", cr.ID, "test-model", []string{"test-model"}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.Activate(active.ID); err != nil {
				t.Fatal(err)
			}
			m := newModel(st, "test")
			m.tool = m.findTool(tc.tool)
			m.width, m.height = 120, 30
			m.view = viewProfiles
			m.loadProfiles("other")
			// The sentence wraps at the pane width, so check the stable prefix.
			view := m.View()
			if got := strings.Contains(view, "Not active"); got != tc.wantNote {
				t.Errorf("%s: note shown = %v, want %v", tc.tool, got, tc.wantNote)
			}
			if !strings.Contains(view, tc.wantStatus) {
				t.Errorf("%s: status missing %q", tc.tool, tc.wantStatus)
			}
			if strings.Contains(view, tc.reject) {
				t.Errorf("%s: status should not contain %q", tc.tool, tc.reject)
			}
			if tc.tool == "pi" {
				m.loadProfiles("active")
				if view := m.View(); !strings.Contains(view, "✓ default · registered") {
					t.Errorf("pi active status missing the aggregated default label")
				}
			}
		})
	}
}
