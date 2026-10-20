package tui

import (
	"strings"
	"time"

	"charon/internal/catalog"
	"charon/internal/tools"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type view int

const (
	viewTools view = iota
	viewProfiles
	viewModels
	viewModelEndpoint
	viewModelSlug
	viewModelWindow   // custom context-window entry
	viewWindowPresets // typical context windows, with Custom… and Unknown
	viewModelEffort
	viewAddEndpoint   // wizard: enter endpoint
	viewAddKey        // wizard: enter API key
	viewFetching      // wizard: fetching models
	viewPickModel     // wizard: choose a model
	viewReviewModels  // wizard: model list + per-row context editor
	viewAddName       // wizard: name the binding
	viewDupName       // clone: name the duplicate
	viewCopyTool      // duplicate: choose the destination tool
	viewEditForm      // edit: field picker (url/name/token/model)
	viewEditField     // edit: single-field text input
	viewBindingDetail // bindings: read-only detail, opened with v on narrow screens
)

// statusLevel colors the footer status line by severity.
type statusLevel int

const (
	statusInfo statusLevel = iota // neutral / muted
	statusOK                      // success (green)
	statusErr                     // failure (red)
	statusWarn                    // caution (yellow): unsaved edits, destructive prompts
)

const (
	addSentinel   = "\x00add"   // the "add new" list row
	sepSentinel   = "\x00sep"   // a blank divider row (inert; cursor skips it)
	libSentinel   = "\x00lib"   // the model-library row on the tools screen
	hdrSentinel   = "\x00hdr"   // the table's column-label row (inert)
	driftSentinel = "\x00drift" // prefix of a drift annotation row (inert)
	noneSentinel  = "\x00none"  // the "No models yet" placeholder row (inert)
)

// isSentinel reports whether v is a synthetic row rather than a binding. Every
// sentinel starts with NUL, which validateName rejects in binding names, so
// the prefix alone separates data rows from chrome.
func isSentinel(v string) bool {
	return strings.HasPrefix(v, "\x00")
}

// inertSentinel reports whether v is a synthetic row the cursor may not rest
// on and no key acts on: the divider, the table's column labels, the "No
// models yet" placeholder, and drift annotations. The add and model-library
// rows are sentinels too, but the cursor may park on them.
func inertSentinel(v string) bool {
	if v == addSentinel || v == libSentinel {
		return false
	}
	return strings.HasPrefix(v, "\x00")
}

type item struct {
	title, desc string
	value       string
	active      bool     // the already-picked binding — stays primary-colored even off-cursor
	hidden      bool     // kept in charon, left out of the tool's config — renders muted
	cells       []string // table columns; nil renders the row the classic way
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

// Contextual key bindings shown in the list's help footer (and "?"-expanded).
var (
	keySwitch  = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch"))
	keyReapply = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reapply"))
	keyEdit    = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit"))
	keyDetails = key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "details"))
	// keyDuplicateRow covers both former shortcuts: cloning within a tool and
	// copying to another one are the same action with a different destination.
	keyDuplicateRow = key.NewBinding(key.WithKeys("c", "x"), key.WithHelp("c", "duplicate"))
	keyHide         = key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "hide"))
	keyDelete       = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	keyAdd          = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add"))
	keyBack         = key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc", "back"))
	keyQuit         = key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q/esc", "quit"))
	keyOpen         = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
	keyChoose       = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose"))
	keyToggle       = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle"))
	keyToggleAll    = key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "all"))
	// keyFilter never matches a real press; it only advertises type-to-search.
	keyFilter = key.NewBinding(key.WithKeys("\x00filter"), key.WithHelp("type", "search"))
	// keyTypeCustom advertises that a typed digit edits a custom context size.
	keyTypeCustom = key.NewBinding(key.WithKeys("\x00type-window"), key.WithHelp("type", "custom"))
	keyRefresh    = key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh"))
	keyManual     = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "type ids"))
	keyWindow     = key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "context"))
	keyEffort     = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "effort"))
	// keyEsc names only esc: on a text input "q" has to stay a letter.
	keyEsc          = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keyClearFilter  = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter"))
	keyMove         = key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓/j/k", "move"))
	keyNextField    = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field"))
	keySelectAction = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))
	keySaveAction   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save"))
	keyContinue     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue"))
	keyCancel       = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	keyConfirm      = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "delete"))
	keyFinish       = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "finish"))
	keyDuplicate    = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "duplicate"))

	keyConfirmReview = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm"))
	keyAddModel      = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "add model"))
	keyCancelAdd     = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	keyEditContext   = key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "context"))
	keyEditRow       = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "model"))
	keyDeleteRow     = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove"))
	keySave          = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save"))
	keyFetch         = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fetch models"))
	keyHelp          = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help"))
	keyCloseHelp     = key.NewBinding(key.WithKeys("esc", "?"), key.WithHelp("esc/?", "close"))
	keyForceQuit     = key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit"))
)

// exampleEndpoint is placeholder text; a real endpoint is never prefilled.
const exampleEndpoint = "https://api.example.com/v1"

// manualModelPlaceholder explains that only ids belong in the add field: context
// is a column in the list behind it, so ids with colons stay intact.
const manualModelPlaceholder = "model id (e.g. glm-4.6, llama3:8b; commas separate several)"

type model struct {
	cat           *catalog.Catalog
	allTools      []*tools.Tool // registry built once; reused across renders
	view          view
	list          list.Model
	input         textinput.Model
	tool          *tools.Tool
	wiz           wizard
	editField     string // which field the single-field editor is editing
	fromForm      bool   // model picker/fetch was launched from the edit form
	delTarget     string // binding name pending delete confirmation
	dupSource     string // binding being duplicated (destination chosen next)
	dupTool       string // chosen duplicate destination ("" until a row is picked)
	showConfirm   bool   // when true, render a confirmation dialog over the binding list
	configSummary string // read-only on-disk values, refreshed with the binding list

	footerKeys []key.Binding // keys the current screen answers to, shown in the bottom legend

	formInputs      []textinput.Model // native form inputs for Name, URL, Token, Model
	formFocus       int               // focusName through focusCancel; non-Pi forms skip focusPiAPI
	formBaseline    formSnapshot      // values when the form opened; esc compares against them
	formBaselineSet bool              // false until the first load captures the baseline
	discardArmed    bool              // first esc on a dirty form armed discarding it
	piAPIOpen       bool
	piAPICursor     int
	fetchReturn     view // screen to return to after fetching models
	showHelp        bool // the ? shortcut sheet is open

	spinner    spinner.Model
	loadingMsg string      // playful line shown on the loading screen, picked per fetch
	pending    *fetchedMsg // fetch result held back until the min-load window elapses
	fetchStart time.Time   // when the current fetch began, for the min-load throttle

	allModels          []string      // full fetched model list, unfiltered
	editTarget         string        // model whose context window the input view is editing
	windowPresetWizard bool          // presets edit the wizard's pending windows, not the catalog
	modelFilter        string        // current type-to-search query in the model picker
	editingModel       catalog.Model // model being added or edited in the local library
	modelEditEndpoint  string        // the endpoint being edited for that model
	reviewCursor       int           // highlighted row in viewReviewModels
	addingModel        bool          // the model list's inline add field has focus
	editingCtx         bool          // the model list's inline ctx field has focus
	editingID          bool          // the model list's inline id field has focus
	reviewSource       view          // where the model list was opened from (viewEditForm or viewPickModel)
	status             string
	statusLvl          statusLevel
	width              int
	height             int
	version            string
	statusAt           time.Time // when the current footer message was set
}

// statusTickMsg carries the statusAt stamp it was scheduled from, so a stale
// tick from an older message never wipes a newer one.
type statusTickMsg struct{ at time.Time }

// statusTTL is how long a footer message lingers before clearing itself, so a
// "✓ Switched …" never becomes permanent furniture. Screen changes clear it
// sooner anyway; this catches the stay-put case.
const statusTTL = 6 * time.Second

// setStatus records a footer message at the given severity and schedules its expiry.
func (m *model) setStatus(level statusLevel, msg string) {
	m.status = msg
	m.statusLvl = level
	m.statusAt = time.Now()
}

// statusTick renews the auto-clear command: every status change restarts the
// clock, and the model drops the message only when the tick matches the latest.
func (m model) statusTick() tea.Cmd {
	if m.status == "" {
		return nil
	}
	at := m.statusAt
	return tea.Tick(statusTTL, func(time.Time) tea.Msg {
		return statusTickMsg{at: at}
	})
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
	// In the wide layout the on-disk summary lives in the detail pane, so the
	// list does not reserve vertical space for it.
	if m.view == viewProfiles && !m.wideDetail() && m.configSummary != "" {
		header += lipgloss.Height(m.renderConfigSummary()) + 1
	}
	h := m.height - header - footerRows
	if h < 3 {
		h = 3
	}
	listWidth := m.width
	if m.wideDetail() {
		listWidth = m.detailListWidth()
	}
	m.list.SetSize(listWidth, h)
	m.list.KeyMap.Quit.SetEnabled(false)
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
	case viewModelEndpoint, viewModelSlug, viewModelWindow,
		viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField:
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
