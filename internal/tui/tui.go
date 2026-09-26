// Package tui provides the interactive arrow-key menu for charon.
//
// The model is split across a few files in this package:
//   - tui.go     the model, lifecycle (Run/Init/Update) and top-level navigation
//   - views.go   rendering (View, wizard header, prompts, status line)
//   - wizard.go  the add/edit binding flow and confirm-delete prompt
//   - picker.go  the fetch-and-choose-a-model screen
package tui

import (
	"fmt"
	"time"

	"charon/internal/catalog"
	"charon/internal/secret"
	"charon/internal/tools"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Run starts the interactive menu against the catalog.
func Run(cat *catalog.Catalog, version string) error {
	_, err := tea.NewProgram(newModel(cat, version), tea.WithAltScreen()).Run()
	return err
}

type view int

const (
	viewTools view = iota
	viewProfiles
	viewAddEndpoint    // wizard: enter endpoint
	viewAddKey         // wizard: enter API key
	viewFetching       // wizard: fetching models
	viewPickModel      // wizard: choose a model
	viewAddCustomModel // wizard: enter custom model ID
	viewAddName        // wizard: name the binding
	viewDupName        // clone: name the duplicate
	viewCopyTool       // copy: choose the destination tool
	viewEditForm       // edit: field picker (url/name/token/model)
	viewEditField      // edit: single-field text input
)

// statusLevel colors the footer status line by severity.
type statusLevel int

const (
	statusInfo statusLevel = iota // neutral / muted
	statusOK                      // success (green)
	statusErr                     // failure (red)
)

const (
	addSentinel = "\x00add" // the "add new" list row
	sepSentinel = "\x00sep" // a blank divider row (inert; cursor skips it)
)

// isSentinel reports whether v is a synthetic action row rather than a binding.
func isSentinel(v string) bool {
	return v == addSentinel || v == sepSentinel
}

type item struct {
	title, desc string
	value       string
	active      bool // the already-picked binding — stays primary-colored even off-cursor
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

// Contextual key bindings shown in the list's help footer (and "?"-expanded).
var (
	keySwitch    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch"))
	keyEdit      = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit"))
	keyBackup    = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clone"))
	keyCopy      = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "copy"))
	keyDelete    = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	keyBack      = key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc", "back"))
	keyQuit      = key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q/esc", "quit"))
	keyOpen      = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	keyChoose    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose"))
	keyToggle    = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle"))
	keyToggleAll = key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "all"))
	// keyFilter never matches a real press; it only advertises type-to-search.
	keyFilter  = key.NewBinding(key.WithKeys("\x00filter"), key.WithHelp("type", "search"))
	keyRefresh = key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh"))
	keyManual  = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "type ids"))
	// keyEsc names only esc: on a text input "q" has to stay a letter.
	keyEsc          = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keyClearFilter  = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter"))
	keyMove         = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move"))
	keyNextField    = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field"))
	keySelectAction = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))
	keySaveAction   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save"))
	keyContinue     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue"))
	keyCancel       = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	keyConfirm      = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "delete"))
	keyFinish       = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "finish"))
	keyDuplicate    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "duplicate"))
	keyRegister     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "register"))
)

// exampleEndpoint is placeholder text; a real endpoint is never prefilled.
const exampleEndpoint = "https://api.example.com/v1"

type model struct {
	cat         *catalog.Catalog
	allTools    []*tools.Tool // registry built once; reused across renders
	view        view
	list        list.Model
	input       textinput.Model
	tool        *tools.Tool
	wiz         wizard
	editField   string // which field the single-field editor is editing
	fromForm    bool   // model picker/fetch was launched from the edit form
	delTarget   string // binding name pending delete confirmation
	dupSource   string // binding being duplicated
	copySource  string // binding being copied to another tool
	showConfirm bool   // when true, render a confirmation dialog over the binding list

	footerKeys []key.Binding // keys the current screen answers to, shown in the bottom legend

	formInputs []textinput.Model // native form inputs for Name, URL, Token, Model
	formFocus  int               // index of focused form element (0: Name, 1: URL, 2: Token, 3: Model, 4: Save, 5: Cancel)

	spinner    spinner.Model
	loadingMsg string      // playful line shown on the loading screen, picked per fetch
	pending    *fetchedMsg // fetch result held back until the min-load window elapses
	fetchStart time.Time   // when the current fetch began, for the min-load throttle

	allModels   []string // full fetched model list, unfiltered
	modelFilter string   // current type-to-search query in the model picker
	status      string
	statusLvl   statusLevel
	width       int
	height      int
	version     string
}

// setStatus records a footer message at the given severity.
func (m *model) setStatus(level statusLevel, msg string) {
	m.status = msg
	m.statusLvl = level
}

// clearStatus wipes the footer message.
func (m *model) clearStatus() {
	m.status = ""
	m.statusLvl = statusInfo
}

// findTool returns the registered tool with the given name, or nil.
func (m *model) findTool(name string) *tools.Tool {
	for _, t := range m.allTools {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// footerRows is the chrome every screen keeps at the bottom of the terminal: the
// status line and, under it, the key legend. The status row stays reserved while
// empty, so a message appearing never shifts the legend off the last row.
const footerRows = 2

// resize sizes the list, reserving space for the banner on the tools screen and the
// footer on every screen; the legend then lands on the last row of the terminal.
func (m *model) resize() {
	header := 1
	if m.view == viewTools {
		header = bannerHeight + 1
	}
	h := m.height - header - footerRows
	if h < 3 {
		h = 3
	}
	m.list.SetSize(m.width, h)
}

func newModel(store *catalog.Catalog, version string) model {
	l := list.New(nil, themedDelegate(), 0, 0)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false) // the model draws the legend itself, pinned to the bottom
	l.SetFilteringEnabled(false)
	l.InfiniteScrolling = true
	l.KeyMap.Quit.SetEnabled(false) // "q"/"esc" must not quit; only ctrl+c does
	l.Styles.Title = titleStyle
	l.Styles.TitleBar = l.Styles.TitleBar.Padding(0, 0, 1, 0)
	// Keep the paginator and help footer mostly in the terminal's palette (muted
	// gray labels; bubbles' defaults are fixed RGB grays, so every piece needs
	// the override), but call out the actionable keys themselves in the accent.
	l.Styles.HelpStyle = l.Styles.HelpStyle.Foreground(colorMuted)
	l.Styles.PaginationStyle = l.Styles.PaginationStyle.Foreground(colorMuted)
	l.Styles.ArabicPagination = lipgloss.NewStyle().Foreground(colorMuted)
	l.Styles.NoItems = lipgloss.NewStyle().Foreground(colorMuted)
	l.Help.Styles.ShortKey = l.Help.Styles.ShortKey.Foreground(colorAccent)
	l.Help.Styles.ShortDesc = l.Help.Styles.ShortDesc.Foreground(colorMuted)
	l.Help.Styles.ShortSeparator = l.Help.Styles.ShortSeparator.Foreground(colorMuted)
	l.Help.Styles.FullKey = l.Help.Styles.FullKey.Foreground(colorAccent)
	l.Help.Styles.FullDesc = l.Help.Styles.FullDesc.Foreground(colorMuted)
	l.Help.Styles.FullSeparator = l.Help.Styles.FullSeparator.Foreground(colorMuted)
	l.Help.Styles.Ellipsis = l.Help.Styles.Ellipsis.Foreground(colorMuted)

	ti := textinput.New()
	ti.CharLimit = 200
	// The focused field itself carries the accent color, so it's obvious which
	// input has keyboard focus.
	ti.PromptStyle = ti.PromptStyle.Foreground(colorAccent)
	ti.PlaceholderStyle = ti.PlaceholderStyle.Foreground(colorMuted)
	// A solid, steady block cursor rather than the default blink: blinking
	// alternates between a filled block and plain text, which reads as the
	// cursor flickering in and out rather than marking a stable position.
	ti.Cursor.Style = ti.Cursor.Style.Foreground(colorAccent)
	ti.Cursor.SetMode(cursor.CursorStatic)

	m := model{cat: store, allTools: tools.All(), view: viewTools, list: l, input: ti, spinner: newSpinner(), version: version}
	m.loadTools()
	return m
}

// setFooterKeys records the keys the current screen answers to; View renders them as
// the one-line legend pinned to the bottom row.
func (m *model) setFooterKeys(bindings ...key.Binding) {
	m.footerKeys = bindings
}

// setDelegate installs an item delegate while keeping the Quit key disabled.
// bubbles/list SetDelegate resets KeyMap to DefaultKeyMap(), which re-enables Quit.
func (m *model) setDelegate(d list.ItemDelegate) {
	m.list.SetDelegate(d)
	m.list.KeyMap.Quit.SetEnabled(false)
}

// inputView reports whether the current view is a text-entry step.
func (m model) inputView() bool {
	if m.view == viewEditForm {
		return m.formFocus < formInputCount
	}
	switch m.view {
	case viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField, viewAddCustomModel:
		return true
	}
	return false
}

// selectedBinding returns the highlighted binding row, or false (with a status hint)
// when the cursor is on a sentinel row like "Add new binding" or the divider — the
// shared guard for the e/c/d shortcuts, which only act on real bindings.
func (m *model) selectedBinding() (item, bool) {
	it, ok := m.list.SelectedItem().(item)
	if !ok || isSentinel(it.value) {
		m.setStatus(statusInfo, "select a binding first")
		return item{}, false
	}
	return it, true
}

// selectByValue moves the cursor to the row matching v; a miss keeps the default.
func (m *model) selectByValue(v string) {
	if v == "" {
		return
	}
	for i, it := range m.list.Items() {
		if li, ok := it.(item); ok && li.value == v {
			m.list.Select(i)
			return
		}
	}
}

func (m *model) loadTools() {
	var items []list.Item
	selectedIndex := 0
	for i, t := range m.allTools {
		desc := "not installed — see the README to set it up"
		if t.Detected != nil && t.Detected() {
			info, _ := t.Describe()
			active := "—"
			if b, found, err := m.cat.Active(t.Name); err == nil && found {
				active = b.Name
			}
			desc = fmt.Sprintf("active: %s · %s · %s", active, info.AuthMode, info.Endpoint)
		}
		items = append(items, item{title: t.Title, desc: desc, value: t.Name})
		if m.tool != nil && t.Name == m.tool.Name {
			selectedIndex = i
		}
	}
	m.list.SetItems(items)
	m.list.Select(selectedIndex)
	m.list.Title = "Charon — select a tool"
	m.setFooterKeys(keyOpen, keyQuit)
	m.setDelegate(themedDelegate()) // two-line rows show each tool's status
}

// loadCopyTools shows every other tool that can receive the pending binding.
func (m *model) loadCopyTools() {
	var items []list.Item
	for _, t := range m.allTools {
		if t.Name == m.tool.Name {
			continue
		}
		desc := "not installed — copy will be saved without switching"
		if catalog.SingleModelTools[t.Name] {
			desc = "single model only — list will use the default model"
		}
		if t.Detected != nil && t.Detected() {
			if catalog.SingleModelTools[t.Name] {
				desc = "installed · single model only — list will use the default model"
			} else {
				desc = "installed"
			}
		}
		items = append(items, item{title: t.Title, desc: desc, value: t.Name})
	}
	m.list.SetItems(items)
	m.list.Select(0)
	m.list.Title = "Copy " + m.copySource + " to"
	m.setFooterKeys(keyChoose, keyBack)
	m.setDelegate(themedDelegate())
}

// loadProfiles rebuilds the binding list for the current tool. selectName, if
// non-empty, is the row the cursor should land on (e.g. the binding just
// edited or cloned); otherwise the cursor defaults to the active binding.
// This keeps an edit or clone from silently relocating the cursor onto
// whatever happens to be active — only an explicit switch should do that.
func (m *model) loadProfiles(selectName string) {
	var items []list.Item
	active := ""
	if b, found, err := m.cat.Active(m.tool.Name); err == nil && found {
		active = b.Name
	}
	saved, _ := m.cat.Bindings(m.tool.Name)
	target := selectName
	if target == "" {
		target = active
	}

	if m.tool.ApplyAuth != nil {
		items = append(items, item{title: "＋ Add new binding…", desc: "Save an endpoint, key, and model (or press 'a')", value: addSentinel})
		if len(saved) > 0 {
			items = append(items, item{value: sepSentinel})
		}
	}

	selectedIndex := 0
	offset := 0
	if m.tool.ApplyAuth != nil {
		offset = 1
		if len(saved) > 0 {
			offset = 2
		}
	}

	for i, b := range saved {
		title := b.Name
		isActive := b.Name == active
		if isActive {
			title = "✓ " + title
		}
		if b.Name == target {
			selectedIndex = i + offset
		}
		items = append(items, item{title: title, desc: m.profileDetail(b.Name), value: b.Name, active: isActive})
	}

	m.list.SetItems(items)
	m.list.Select(selectedIndex)
	m.list.Title = m.tool.Title + " bindings"
	m.setFooterKeys(keySwitch, keyEdit, keyBackup, keyCopy, keyDelete, keyBack)
	m.setDelegate(themedDelegate())
	if len(saved) == 0 && m.status == "" && m.tool.ApplyAuth != nil {
		m.setStatus(statusInfo, `No bindings yet — press enter on "Add new binding" or press 'a' to create one.`)
	}
}

// profileDetail is the second-line summary of a binding: its endpoint and default
// model, plus how many extra models its picker offers. For the active binding the
// live model and effort are overlaid for display only — they are not written back.
func (m *model) profileDetail(name string) string {
	b, found, err := m.cat.BindingByName(m.tool.Name, name)
	if err != nil || !found {
		return ""
	}
	url, slug := "", ""
	extra := 0
	if cr, err := m.cat.Credential(b.CredentialID); err == nil {
		if p, err := m.cat.Provider(cr.ProviderID); err == nil {
			url = p.BaseURL
		}
	}
	if s, err := m.cat.ModelSlug(b.ModelID); err == nil {
		slug = s
	}
	if slugs, err := m.cat.ModelSlugs(b.Models); err == nil && len(slugs) > 1 {
		extra = len(slugs) - 1
	}
	effort := ""
	if active, ok, err := m.cat.Active(m.tool.Name); err == nil && ok && active.Name == name && m.tool.Describe != nil {
		if info, err := m.tool.Describe(); err == nil {
			if info.Endpoint != "" {
				url = info.Endpoint
			}
			if info.Model != "" {
				slug = info.Model
			}
			effort = info.Effort
		}
	}
	if url == "" {
		url = "default endpoint"
	}
	if slug == "" {
		slug = "no model"
	}
	detail := url + " · " + slug
	if extra > 0 {
		detail += fmt.Sprintf(" +%d", extra)
	}
	if effort != "" {
		detail += " · effort: " + effort
	}
	return detail
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case fetchedMsg:
		// Hold a too-fast result until minLoadDuration so the loading screen never flickers.
		if elapsed := time.Since(m.fetchStart); elapsed < minLoadDuration {
			m.pending = &msg
			return m, tea.Tick(minLoadDuration-elapsed, func(time.Time) tea.Msg { return minLoadElapsedMsg{} })
		}
		return m.applyFetched(msg)

	case minLoadElapsedMsg:
		if m.pending == nil {
			return m, nil
		}
		result := *m.pending
		m.pending = nil
		return m.applyFetched(result)

	case spinner.TickMsg:
		if m.view != viewFetching {
			return m, nil // ignore stray ticks once we've left the loading screen
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		// ctrl+c is the only way to quit, from any screen.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.view == viewEditForm {
			return m.updateEditForm(msg)
		}
		if m.inputView() {
			return m.updateInput(msg)
		}
		if m.showConfirm {
			return m.handleConfirmDelete(msg)
		}
		if m.view == viewPickModel {
			return m.updatePickModel(msg)
		}
		switch msg.String() {
		case "esc":
			return m.onEsc()
		case "q":
			if m.view == viewTools || m.view == viewProfiles {
				return m.onEsc()
			}
		case "enter":
			return m.onEnter()
		case "a":
			if m.view == viewProfiles {
				m.wiz = wizard{}
				m.view = viewEditForm
				m.clearStatus()
				m.loadEditForm()
				return m, nil
			}
		case "e":
			if m.view == viewEditForm {
				// Inside the edit form, "e" opens the highlighted field for editing.
				if it, ok := m.list.SelectedItem().(item); ok {
					return m.onEditFormSelect(it.value)
				}
				return m, nil
			}
			if m.view == viewProfiles && m.tool.ApplyAuth != nil {
				return m.onEditKey()
			}
		case "c":
			if m.view == viewProfiles {
				if it, ok := m.selectedBinding(); ok {
					return m.startBackup(it.value)
				}
				return m, nil
			}
		case "x":
			if m.view == viewProfiles {
				if it, ok := m.selectedBinding(); ok {
					m.copySource = it.value
					m.view = viewCopyTool
					m.clearStatus()
					m.loadCopyTools()
					return m, nil
				}
			}
		case "d":
			if m.view == viewProfiles {
				return m.onDeleteKey()
			}
		}
	}

	before := m.list.Index()
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if m.view == viewProfiles {
		m.skipSeparators(before)
	}
	return m, cmd
}

// onEditKey opens the edit form for the highlighted binding ("e").
func (m model) onEditKey() (tea.Model, tea.Cmd) {
	it, ok := m.selectedBinding()
	if !ok {
		return m, nil
	}
	b, found, err := m.cat.BindingByName(m.tool.Name, it.value)
	if err != nil || !found {
		m.setStatus(statusErr, "no binding named "+it.value)
		return m, nil
	}
	cr, err := m.cat.Credential(b.CredentialID)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	p, err := m.cat.Provider(cr.ProviderID)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	slug, _ := m.cat.ModelSlug(b.ModelID)
	slugs, _ := m.cat.ModelSlugs(b.Models)
	m.wiz = wizard{name: it.value, origName: it.value, edit: true,
		endpoint: p.BaseURL, key: cr.Key, model: slug, models: slugs}
	m.editField = ""
	m.view = viewEditForm
	m.clearStatus()
	m.loadEditForm()
	return m, nil
}

// onDeleteKey arms the confirm-delete prompt for the highlighted binding ("d").
// The active binding is not deletable; switch away first.
func (m model) onDeleteKey() (tea.Model, tea.Cmd) {
	it, ok := m.selectedBinding()
	if !ok {
		return m, nil
	}
	if active, found, err := m.cat.Active(m.tool.Name); err == nil && found && active.Name == it.value {
		m.setStatus(statusErr, fmt.Sprintf("cannot delete active binding %q; switch to another binding first", it.value))
		return m, nil
	}
	m.delTarget = it.value
	m.showConfirm = true
	m.clearStatus()
	return m, nil
}

// startBackup clones the highlighted binding ("c") into the first free
// "<name>-copy" (then "<name>-copy-2", …). The clone shares the credential and
// model list and is not activated.
func (m model) startBackup(name string) (tea.Model, tea.Cmd) {
	b, found, err := m.cat.BindingByName(m.tool.Name, name)
	if err != nil || !found {
		m.setStatus(statusErr, "no binding named "+name)
		return m, nil
	}
	saved, err := m.cat.Bindings(m.tool.Name)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	names := make([]string, len(saved))
	for i, s := range saved {
		names[i] = s.Name
	}
	newName := nextDuplicateName(names, name)
	slugs, err := m.cat.ModelSlugs(b.Models)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	slug, err := m.cat.ModelSlug(b.ModelID)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	if _, err := m.cat.AddBinding(b.Tool, newName, b.CredentialID, slug, slugs); err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	m.loadProfiles(newName)
	m.setStatus(statusOK, fmt.Sprintf("Cloned %s as %s", name, newName))
	return m, nil
}

// copyBindingToTool copies a saved binding to another tool without activating it.
func (m model) copyBindingToTool(src, toolName string) error {
	dstTool := m.findTool(toolName)
	if dstTool == nil {
		return fmt.Errorf("unknown tool %s", toolName)
	}
	b, found, err := m.cat.BindingByName(m.tool.Name, src)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no binding named %s", src)
	}
	cr, err := m.cat.Credential(b.CredentialID)
	if err != nil {
		return err
	}
	p, err := m.cat.Provider(cr.ProviderID)
	if err != nil {
		return err
	}
	slug, err := m.cat.ModelSlug(b.ModelID)
	if err != nil {
		return err
	}
	slugs, err := m.cat.ModelSlugs(b.Models)
	if err != nil {
		return err
	}
	if dstTool.Name != b.Tool && catalog.SingleModelTools[dstTool.Name] {
		slugs = []string{slug}
	}
	saved, err := m.cat.Bindings(dstTool.Name)
	if err != nil {
		return err
	}
	names := make([]string, len(saved))
	for i, row := range saved {
		names[i] = row.Name
	}
	dst := nextDuplicateName(names, src)
	_, err = catalog.StoreBinding(m.cat, dstTool, nil, dst, p.BaseURL, cr.Key, slug, slugs, true)
	return err
}

// nextDuplicateName returns the first free "<src>-copy" (then "<src>-copy-2", …).
func nextDuplicateName(existing []string, src string) string {
	taken := make(map[string]bool, len(existing))
	for _, n := range existing {
		taken[n] = true
	}
	base := src + "-copy"
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !taken[cand] {
			return cand
		}
	}
}

// skipSeparators nudges the cursor off an inert divider row after a move, continuing
// in the direction of travel so the blank gap never traps the selection.
func (m *model) skipSeparators(before int) {
	if it, ok := m.list.SelectedItem().(item); !ok || it.value != sepSentinel {
		return
	}
	if m.list.Index() >= before {
		m.list.CursorDown()
	} else {
		m.list.CursorUp()
	}
}

// startInput configures the text field for a step (password masks the echo).
func (m *model) startInput(placeholder string, password bool) {
	m.input.SetValue("")
	m.input.Placeholder = placeholder
	if password {
		m.input.EchoMode = textinput.EchoPassword
	} else {
		m.input.EchoMode = textinput.EchoNormal
	}
	m.input.Focus()
}

func (m model) onEsc() (tea.Model, tea.Cmd) {
	switch m.view {
	case viewTools:
		return m, tea.Quit
	case viewProfiles:
		m.view = viewTools
		m.clearStatus()
		m.loadTools()
		m.resize() // banner returns → shrink the list
		return m, nil
	case viewCopyTool:
		m.view = viewProfiles
		m.clearStatus()
		m.loadProfiles(m.copySource)
		return m, nil
	case viewEditForm:
		m.editField = ""
		m.dupSource = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles("")
		return m, nil
	case viewPickModel:
		m.view = viewEditForm
		m.loadEditFormAt(focusFetch)
		return m, nil
	}
	return m, nil
}

func (m model) onEnter() (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if it.value == sepSentinel {
		return m, nil // the blank divider is inert
	}
	switch m.view {
	case viewTools:
		t := m.findTool(it.value)
		if t == nil || t.Detected == nil || !t.Detected() {
			m.setStatus(statusInfo, it.title+" isn't installed yet — see the README to set it up")
			return m, nil
		}
		m.tool = t
		m.view = viewProfiles
		m.clearStatus()
		m.loadProfiles("") // land on the active binding
		m.resize()         // banner hidden → grow the list

	case viewCopyTool:
		if err := m.copyBindingToTool(m.copySource, it.value); err != nil {
			m.setStatus(statusErr, err.Error())
			return m, nil
		}
		m.view = viewProfiles
		m.setStatus(statusOK, "Copied "+m.copySource+" to "+it.title)
		m.loadProfiles(m.copySource)
		return m, nil

	case viewProfiles:
		if it.value == addSentinel {
			m.wiz = wizard{}
			m.view = viewEditForm
			m.loadEditForm()
			return m, nil
		}
		b, found, err := m.cat.BindingByName(m.tool.Name, it.value)
		if err != nil || !found {
			m.setStatus(statusErr, "no binding named "+it.value)
			return m, nil
		}
		if _, err := m.cat.Activate(b.ID); err != nil {
			m.setStatus(statusErr, err.Error())
		} else {
			info, _ := m.tool.Describe()
			m.setStatus(statusOK, fmt.Sprintf("Switched to %s (%s · %s)",
				it.value, info.Endpoint, secret.Mask(info.Secret)))
			m.loadProfiles(it.value)
		}

	case viewEditForm:
		// "e" or enter edits the highlighted field; esc saves & backs out.
		it, ok := m.list.SelectedItem().(item)
		if ok {
			return m.onEditFormSelect(it.value)
		}
		return m, nil

	}
	return m, nil
}
