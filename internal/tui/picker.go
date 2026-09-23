package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"charon/internal/models"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

// minLoadDuration is the floor the loading screen stays up so a fast fetch doesn't flicker.
const minLoadDuration = 1 * time.Second

// fetchedMsg carries the async result of a models.Fetch call.
type fetchedMsg struct {
	list []string
	err  error
}

// minLoadElapsedMsg fires when the min-load window closes on a result parked in model.pending.
type minLoadElapsedMsg struct{}

func fetchModelsCmd(provider, endpoint, key string) tea.Cmd {
	return func() tea.Msg {
		l, err := probeModels(provider, endpoint, key)
		return fetchedMsg{list: l, err: err}
	}
}

// probeModels asks the endpoint for its model list in the tool's historical dialect
// first, then the other. Which dialect answered is not stored.
func probeModels(provider, endpoint, key string) ([]string, error) {
	first := models.Provider(provider)
	list, err := models.Fetch(first, endpoint, key)
	if err == nil {
		return list, nil
	}
	other := models.OpenAI
	if first == models.OpenAI {
		other = models.Anthropic
	}
	if list, err2 := models.Fetch(other, endpoint, key); err2 == nil {
		return list, nil
	}
	return nil, err
}

// loadingMessages are playful lines shown while fetching, one picked at random per fetch.
var loadingMessages = []string{
	"fetching models, almost there…",
	"ferrying your request across the Styx…",
	"summoning the model list…",
	"asking the endpoint nicely…",
	"warming up the engines…",
	"charting the crossing…",
	"counting the models…",
	"reticulating splines…",
	"the ferry is departing, hold tight…",
	"hang on, nearly across…",
}

func randomLoadingMsg() string {
	return loadingMessages[rand.Intn(len(loadingMessages))]
}

// beginFetch shows the loading screen and starts the throttle, spinner, and model fetch.
func (m *model) beginFetch() tea.Cmd {
	m.view = viewFetching
	m.fetchStart = time.Now()
	m.pending = nil
	m.loadingMsg = randomLoadingMsg()
	m.spinner = newSpinner()
	return tea.Batch(m.spinner.Tick, fetchModelsCmd(m.tool.Provider, m.tool.ResolveEndpoint(m.wiz.endpoint), m.wiz.key))
}

// applyFetched moves to the model picker on success. On failure it falls through to
// manual entry rather than giving up on models: plenty of endpoints (self-hosted
// relays, gateways behind auth) serve chat fine but expose no /v1/models, and their
// model ids are exactly what the user needs registered.
func (m model) applyFetched(msg fetchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus(statusErr, msg.err.Error()+" — type the model ids instead")
		return m.startManualModels()
	}
	m.view = viewPickModel
	m.setStatus(statusInfo, fmt.Sprintf("%d models found", len(msg.list)))
	m.showModels(msg.list)
	return m, nil
}

// startManualModels opens the type-the-ids screen, prefilled with whatever the binding
// already registers so an edit doesn't have to retype the list. Reached both from the
// picker's "✎ Enter model IDs" row and from a failed fetch.
func (m model) startManualModels() (tea.Model, tea.Cmd) {
	m.view = viewAddCustomModel
	m.startInput("model ids, e.g. gpt-4o, kimi-k2, deepseek-v3", false)
	if prefill := strings.Join(m.wiz.modelIDs(), ", "); prefill != "" {
		m.input.SetValue(prefill)
	}
	return m, textinput.Blink
}

// filterModels fuzzy-matches query against model ids, ranked best-first (empty = all).
func filterModels(all []string, query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return all
	}
	matches := fuzzy.Find(q, all)
	out := make([]string, len(matches))
	for i, mt := range matches {
		out[i] = mt.Str
	}
	return out
}

// showModels installs a freshly fetched model list and resets the search query.
func (m *model) showModels(ids []string) {
	m.allModels = ids
	m.modelFilter = ""
	// A re-fetch from a different endpoint (or a refreshed catalog) can drop ids that
	// were previously checked. Keep only those still offered, so the picker never
	// registers a model the endpoint no longer lists. An empty remainder falls back
	// to registering the whole fetched list (pickerModels).
	if len(m.wiz.models) > 0 {
		offered := make(map[string]bool, len(ids))
		for _, id := range ids {
			offered[id] = true
		}
		kept := m.wiz.models[:0]
		for _, id := range m.wiz.models {
			if offered[id] {
				kept = append(kept, id)
			}
		}
		m.wiz.models = kept
	}
	m.renderModels()
}

// toggleModel adds or removes a model id from the curated picker list, keeping
// first-seen order so the list reads in the order the user checked things off.
func (m *model) toggleModel(id string) {
	for i, sel := range m.wiz.models {
		if sel == id {
			m.wiz.models = append(m.wiz.models[:i:i], m.wiz.models[i+1:]...)
			return
		}
	}
	m.wiz.models = append(m.wiz.models[:len(m.wiz.models):len(m.wiz.models)], id)
}

// toggleAllModels selects all models when some or none are selected, or clears the
// selection when every model in the current view is already selected. If a filter
// is active, it only operates on the matching subset.
func (m *model) toggleAllModels() {
	targetIDs := filterModels(m.allModels, m.modelFilter)
	if len(targetIDs) == 0 {
		return
	}

	allSelected := true
	for _, id := range targetIDs {
		if !m.modelSelected(id) {
			allSelected = false
			break
		}
	}

	if allSelected {
		targetSet := make(map[string]bool, len(targetIDs))
		for _, id := range targetIDs {
			targetSet[id] = true
		}
		var kept []string
		for _, id := range m.wiz.models {
			if !targetSet[id] {
				kept = append(kept, id)
			}
		}
		m.wiz.models = kept
	} else {
		for _, id := range targetIDs {
			if !m.modelSelected(id) {
				m.wiz.models = append(m.wiz.models, id)
			}
		}
	}
}

// modelSelected reports whether id is in the curated picker list.
func (m *model) modelSelected(id string) bool {
	for _, sel := range m.wiz.models {
		if sel == id {
			return true
		}
	}
	return false
}

// pickerSummary describes the curated list for the footer, so it's clear what checking
// rows accomplishes and how to finish.
func (m *model) pickerSummary() string {
	n := len(m.wiz.models)
	if n == 0 {
		return "selection cleared — the whole fetched list will be registered"
	}
	return fmt.Sprintf("%d selected (default: %s) · enter on a row makes it the default, or choose Done",
		n, m.defaultModelLabel())
}

// defaultModelLabel names the model that will be requested by default: the explicitly
// picked one, else the first checked id (what normalized() would promote), else none.
func (m *model) defaultModelLabel() string {
	if m.wiz.model != "" {
		return m.wiz.model
	}
	if len(m.wiz.models) > 0 {
		return m.wiz.models[0]
	}
	return "tool default"
}

// landingValue is the row the picker should open on: the current default model, else the
// Done row while a selection is pending, else the first available model or custom entry.
func (m *model) landingValue() string {
	if m.wiz.model != "" {
		return m.wiz.model
	}
	if m.tool != nil && m.tool.ModelMenu != "" && len(m.wiz.models) > 0 {
		return doneModels
	}
	if len(m.allModels) > 0 {
		return m.allModels[0]
	}
	return customModel
}

// indexOfValue returns the position of the row carrying value, or 0 when absent (e.g.
// a previously chosen model the latest fetch no longer lists).
func indexOfValue(items []list.Item, value string) int {
	for i, it := range items {
		if row, ok := it.(item); ok && row.value == value {
			return i
		}
	}
	return 0
}

// modelRowTitle prefixes a model row with its state: "✓" for the binding's default
// model, "•" for a row checked into the curated picker list, two spaces otherwise so
// unmarked ids stay aligned with marked ones.
func modelRowTitle(id string, isDefault, isChecked bool) string {
	switch {
	case isDefault:
		return "✓ " + id
	case isChecked:
		return "• " + id
	default:
		return "  " + id
	}
}

// renderModels rebuilds the picker rows for the current query (echoed in the title).
func (m *model) renderModels() {
	ids := filterModels(m.allModels, m.modelFilter)
	var items []list.Item

	hasModelMenu := m.tool != nil && m.tool.ModelMenu != ""

	// With models checked, the list needs an explicit way out: otherwise the only way to
	// leave is to press enter on some row, which also re-picks the default model.
	// Only tools that support registered model lists have a curated selection to finish.
	if hasModelMenu {
		if n := len(m.wiz.models); n > 0 {
			items = append(items, item{
				title: fmt.Sprintf("✔ Done — register these %d model(s)", n),
				desc:  "Default: " + m.defaultModelLabel(),
				value: doneModels,
			})
		}
	}
	if m.modelFilter == "" {
		items = append(items, item{title: "← Back (change URL / key)", desc: "", value: backModel})
	}
	items = append(items, item{title: "✎ Enter custom model IDs...", desc: "Type unlisted ids, comma-separated", value: customModel})
	items = append(items, item{value: sepSentinel})

	for _, id := range ids {
		isChosen := id == m.wiz.model
		isChecked := hasModelMenu && m.modelSelected(id)
		items = append(items, item{title: modelRowTitle(id, isChosen, isChecked), desc: "", value: id, active: isChosen})
	}
	// A blank divider sets the action rows apart from the models (only when not searching).
	if len(ids) > 0 && m.modelFilter == "" {
		items = append(items, item{value: sepSentinel})
	}
	m.list.SetItems(items)
	// No search: land on the row the current state points at; while searching, the best
	// match is on top.
	selectedIndex := 0
	if m.modelFilter == "" {
		selectedIndex = indexOfValue(items, m.landingValue())
	}
	m.list.Select(selectedIndex)
	title := m.tool.Title + " — choose a model"
	if hasModelMenu {
		if n := len(m.wiz.models); n > 0 {
			title += fmt.Sprintf(" · %d in picker", n)
		}
	}
	if m.modelFilter != "" {
		title += fmt.Sprintf(" · search: %s (%d matches)", m.modelFilter, len(ids))
	}
	m.list.Title = title
	if m.tool.ModelMenu != "" {
		m.setHelpKeys(keyChoose, keyToggle, keyToggleAll, keyFilter, keyRefresh, keyBack)
	} else {
		m.setHelpKeys(keyChoose, keyFilter, keyRefresh, keyBack)
	}
	m.setDelegate(themedCompactDelegate())
}

// updatePickModel drives the picker: printable keys search, space checks the
// highlighted model into the curated list, ctrl+a toggles all models, ctrl+r refetches,
// nav keys fall through to the list, enter/esc choose or cancel.
func (m model) updatePickModel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if m.modelFilter != "" {
			m.modelFilter = ""
			m.renderModels()
			return m, nil
		}
		return m.onEsc()
	case tea.KeyEnter:
		return m.onEnter()
	case tea.KeyCtrlR:
		if m.wiz.endpoint == "" || m.wiz.key == "" {
			m.setStatus(statusInfo, "set URL and token first, then refresh")
			return m, nil
		}
		cmd := m.beginFetch()
		return m, cmd
	case tea.KeyBackspace:
		if r := []rune(m.modelFilter); len(r) > 0 {
			m.modelFilter = string(r[:len(r)-1])
			m.renderModels()
		}
		return m, nil
	case tea.KeyRunes:
		m.modelFilter += string(msg.Runes)
		m.renderModels()
		return m, nil
	case tea.KeyCtrlA:
		// Ctrl+A toggles selection for all models (or all filtered matches).
		if m.tool.ModelMenu == "" {
			m.setStatus(statusInfo, m.tool.Title+" can't be given a model list — press enter to pick one model")
			return m, nil
		}
		if len(m.allModels) == 0 {
			return m, nil
		}
		cursor := m.list.Index()
		m.toggleAllModels()
		m.renderModels()
		m.list.Select(cursor)
		m.setStatus(statusInfo, m.pickerSummary())
		return m, nil
	case tea.KeySpace:
		// Space checks a model into the list registered with the tool's own picker.
		// Model ids don't contain spaces, so this can't cost a useful search query.
		if m.tool.ModelMenu == "" {
			m.setStatus(statusInfo, m.tool.Title+" can't be given a model list — press enter to pick one model")
			return m, nil
		}
		it, ok := m.list.SelectedItem().(item)
		if !ok || isSentinel(it.value) {
			return m, nil
		}
		cursor := m.list.Index()
		m.toggleModel(it.value)
		m.renderModels()
		m.list.Select(cursor) // toggling must not move the cursor off the row just checked
		m.setStatus(statusInfo, m.pickerSummary())
		return m, nil
	}
	// Arrows, page keys, home/end, ctrl+n/ctrl+p: let the list move the cursor.
	before := m.list.Index()
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.skipSeparators(before)
	return m, cmd
}
