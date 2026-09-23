package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charon/internal/catalog"
	"charon/internal/tools"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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

func TestWizardStep(t *testing.T) {
	tests := []struct {
		view      view
		wantN     int
		wantTotal int
		wantLabel string
	}{
		{viewAddEndpoint, 1, 4, "API base URL"},
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
		{customModel, true},
		{backModel, true},
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

func TestInputView(t *testing.T) {
	m := model{}
	for _, v := range []view{viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField, viewAddCustomModel} {
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
	m.formFocus = 4 // Save button, not an input field
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

func TestNewSpinner(t *testing.T) {
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

func TestSetModelFieldParsesCommaSeparatedIDs(t *testing.T) {
	var w wizard

	w.setModelField("gpt-4o")
	if w.model != "gpt-4o" || w.models != nil {
		t.Errorf("single id: model=%q models=%v, want gpt-4o with no curated list", w.model, w.models)
	}

	w.setModelField(" kimi-k2 , glm-4.6 ,, ")
	if w.model != "kimi-k2" || strings.Join(w.models, ",") != "kimi-k2,glm-4.6" {
		t.Errorf("list: model=%q models=%v, want kimi-k2 default over [kimi-k2 glm-4.6]", w.model, w.models)
	}

	// Narrowing to one id changes the default without discarding the curated list —
	// that's what the picker's checkboxes are for.
	w.setModelField("glm-4.6")
	if w.model != "glm-4.6" || strings.Join(w.models, ",") != "kimi-k2,glm-4.6" {
		t.Errorf("re-default: model=%q models=%v, want glm-4.6 over the same list", w.model, w.models)
	}

	w.setModelField("  ")
	if w.model != "" {
		t.Errorf("blank: model=%q, want empty", w.model)
	}
}

func TestModelRowTitleMarksDefaultAndCheckedRows(t *testing.T) {
	cases := []struct {
		name               string
		isDefault, checked bool
		want               string
	}{
		{"default model", true, false, "✓ m"},
		{"checked into picker", false, true, "• m"},
		{"default wins over checked", true, true, "✓ m"},
		{"plain row stays aligned", false, false, "  m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelRowTitle("m", tc.isDefault, tc.checked); got != tc.want {
				t.Errorf("modelRowTitle = %q, want %q", got, tc.want)
			}
		})
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
	if got.view != viewAddCustomModel {
		t.Errorf("view = %v, want the manual model-id entry screen", got.view)
	}
	if got.statusLvl != statusErr || !strings.Contains(got.status, "type the model ids") {
		t.Errorf("status = %q (level %v), want the error to point at manual entry", got.status, got.statusLvl)
	}
	// The already-registered ids are prefilled so an edit needn't retype them.
	if v := got.input.Value(); v != "kimi-k2, glm-4.6" {
		t.Errorf("input = %q, want the binding's current ids prefilled", v)
	}
}

// TestManualEntryRegistersTypedIDs covers the typed-list path end to end: every id is
// registered and the first becomes the default model.
func TestManualEntryRegistersTypedIDs(t *testing.T) {
	m := pickerModel(t, nil)
	m.wiz = wizard{endpoint: "https://relay.example/v1", key: "sk-x", name: "relay", edit: true}
	m.view = viewAddCustomModel
	m.input.SetValue("glm-4.6, kimi-k2 ,, deepseek-v3")

	next, _ := m.updateInput(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("updateInput returned %T, want model", next)
	}
	if got.wiz.model != "glm-4.6" {
		t.Errorf("default model = %q, want the first typed id", got.wiz.model)
	}
	if strings.Join(got.wiz.models, ",") != "glm-4.6,kimi-k2,deepseek-v3" {
		t.Errorf("models = %v, want every typed id registered", got.wiz.models)
	}
}

// TestSpaceRejectedForToolWithoutModelMenu guards the honest-UI rule: Codex's config
// has nowhere to put a model list, so checking rows must say so rather than silently
// collecting ids that would be dropped on save.
func TestSpaceRejectedForToolWithoutModelMenu(t *testing.T) {
	m := pickerModel(t, []string{"gpt-5.5", "gpt-5.4"})
	for _, tool := range m.allTools {
		if tool.ModelMenu == "" {
			m.tool = tool
			break
		}
	}
	if m.tool.ModelMenu != "" {
		t.Skip("every registered tool exposes a model menu")
	}
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

func TestCodexPickerDoesNotShowDoneRowOrBullets(t *testing.T) {
	m := pickerModel(t, []string{"gpt-5.5", "gpt-5.4"})
	m.tool = &tools.Tool{Title: "Codex", ModelMenu: ""}
	m.wiz.models = []string{"gpt-5.5", "gpt-5.4"}
	m.wiz.model = "gpt-5.5"
	m.renderModels()

	for _, it := range m.list.Items() {
		if row, ok := it.(item); ok {
			if row.value == doneModels {
				t.Error("Codex picker must not show Done row")
			}
			if strings.HasPrefix(row.title, "• ") {
				t.Errorf("Codex picker must not show bullet mark on %q", row.title)
			}
		}
	}
	if strings.Contains(m.list.Title, "in picker") {
		t.Errorf("Codex picker title %q must not show 'in picker'", m.list.Title)
	}
}

func TestModelMenuNote(t *testing.T) {
	m := pickerModel(t, nil)
	if note := m.modelMenuNote(); !strings.Contains(note, m.tool.ModelMenu) {
		t.Errorf("note = %q, want it to name the tool's own menu %q", note, m.tool.ModelMenu)
	}
	m.tool = &tools.Tool{Title: "Codex"}
	if note := m.modelMenuNote(); !strings.Contains(note, "only the first id is used") {
		t.Errorf("note = %q, want it to warn that the list is ignored", note)
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

func TestPickerDoneRow(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6", "deepseek-v3"})

	// Initially no models are checked, so no Done row should exist.
	for _, it := range m.list.Items() {
		if row, ok := it.(item); ok && row.value == doneModels {
			t.Fatal("found Done row before any model was checked")
		}
	}

	// Checking models should surface the Done row at the top.
	m.toggleModel("glm-4.6")
	m.toggleModel("kimi-k2")
	m.renderModels()

	items := m.list.Items()
	if len(items) == 0 {
		t.Fatal("picker items empty")
	}
	top, ok := items[0].(item)
	if !ok || top.value != doneModels {
		t.Fatalf("first item = %+v, want Done row", items[0])
	}
	if !strings.Contains(top.title, "register these 2 model(s)") {
		t.Errorf("Done row title = %q, want it to count 2 models", top.title)
	}
	// Without an explicit default, the first checked model is labeled as default.
	if !strings.Contains(top.desc, "Default: glm-4.6") {
		t.Errorf("Done row desc = %q, want it to name first checked model glm-4.6", top.desc)
	}

	// With an explicit default, that default is described.
	m.wiz.model = "kimi-k2"
	m.renderModels()
	top = m.list.Items()[0].(item)
	if !strings.Contains(top.desc, "Default: kimi-k2") {
		t.Errorf("Done row desc with explicit default = %q, want Default: kimi-k2", top.desc)
	}

	// Pressing Enter on the Done row returns to edit form without modifying choices.
	m.list.Select(0)
	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("updatePickModel returned %T, want model", next)
	}
	if got.view != viewEditForm {
		t.Errorf("view after Enter on Done = %v, want viewEditForm", got.view)
	}
	if got.wiz.model != "kimi-k2" {
		t.Errorf("model = %q, want unchanged kimi-k2", got.wiz.model)
	}
	if strings.Join(got.wiz.models, ",") != "glm-4.6,kimi-k2" {
		t.Errorf("models = %v, want glm-4.6,kimi-k2 preserved", got.wiz.models)
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

	// Rule 2: when models are checked but no explicit default is set, land on Done row.
	m.toggleModel("glm-4.6")
	m.renderModels()
	if got := m.landingValue(); got != doneModels {
		t.Errorf("landingValue() = %q, want %q", got, doneModels)
	}
	if sel, ok := m.list.SelectedItem().(item); !ok || sel.value != doneModels {
		t.Errorf("selected item = %+v, want Done row", m.list.SelectedItem())
	}

	// Rule 3: when a default model is chosen, land on that model row.
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

func TestPickerEnterOnModelSelectsAsDefaultAndAddsToSelection(t *testing.T) {
	m := pickerModel(t, []string{"kimi-k2", "glm-4.6", "deepseek-v3"})
	m.wiz.models = []string{"kimi-k2"}
	m.wiz.model = "kimi-k2"
	m.renderModels()

	// Select glm-4.6 which is not yet in m.wiz.models
	idx := -1
	for i, it := range m.list.Items() {
		if row, ok := it.(item); ok && row.value == "glm-4.6" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("glm-4.6 not found in picker")
	}
	m.list.Select(idx)

	next, _ := m.updatePickModel(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := next.(model)
	if !ok {
		t.Fatalf("updatePickModel returned %T, want model", next)
	}
	if got.view != viewEditForm {
		t.Errorf("view = %v, want viewEditForm", got.view)
	}
	if got.wiz.model != "glm-4.6" {
		t.Errorf("model = %q, want glm-4.6", got.wiz.model)
	}
	if gotStr := strings.Join(got.wiz.models, ","); gotStr != "kimi-k2,glm-4.6" {
		t.Errorf("models = %q, want glm-4.6 joined into [kimi-k2 glm-4.6]", gotStr)
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
