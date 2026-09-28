package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"charon/internal/catalog"
	"charon/internal/models"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

// minLoadDuration is the floor the loading screen stays up so a fast fetch doesn't flicker.
const minLoadDuration = 1 * time.Second

// fetchedMsg carries the async result of a models.Fetch call.
type fetchedMsg struct {
	list []models.Info
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
func probeModels(provider, endpoint, key string) ([]models.Info, error) {
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
	m.showModelInfos(msg.list)
	return m, nil
}

// startManualModels opens the type-the-ids screen, prefilled with whatever the binding
// already registers so an edit doesn't have to retype the list. Reached both from the
// picker's m key and from a failed fetch.
func (m model) startManualModels() (tea.Model, tea.Cmd) {
	m.view = viewAddCustomModel
	m.startInput("e.g. kimi-k3:1m, z-ai/glm-5.3, custom:200k", false)
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
	if m.wiz.windows == nil {
		m.wiz.windows = map[string]int{}
	}
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

func (m *model) showModelInfos(infos []models.Info) {
	m.allModels = nil
	if m.wiz.windows == nil {
		m.wiz.windows = map[string]int{}
	}
	if m.wiz.manualWindows == nil {
		m.wiz.manualWindows = map[string]bool{}
	}
	seen := make(map[string]bool)

	// 1. Fetched remote models
	for _, info := range infos {
		slug := info.ID
		if !seen[slug] {
			m.allModels = append(m.allModels, slug)
			seen[slug] = true
		}
		if _, ok := m.wiz.windows[slug]; !ok {
			if bw := models.DefaultContextWindow(slug); bw > 0 {
				m.wiz.windows[slug] = bw
			}
		}
	}

	// 2. If provider was already registered in catalog, overlay its stored models and windows
	if m.cat != nil && m.tool != nil {
		if p, found, err := m.cat.ProviderByURL(m.tool.ResolveEndpoint(m.wiz.endpoint)); err == nil && found {
			if ms, err := m.cat.ModelsForProvider(p.ID); err == nil {
				for _, stored := range ms {
					if !seen[stored.Slug] {
						m.allModels = append(m.allModels, stored.Slug)
						seen[stored.Slug] = true
					}
					if !m.wiz.manualWindows[stored.Slug] && stored.ContextWindow > 0 {
						m.wiz.windows[stored.Slug] = stored.ContextWindow
						if stored.ContextWindowSource == catalog.WindowManual {
							m.wiz.manualWindows[stored.Slug] = true
						}
					}
				}
			}
		}
	}

	// 3. Keep any unlisted/curated models already in wizard
	var unlisted []string
	for _, id := range m.wiz.models {
		if !seen[id] {
			unlisted = append(unlisted, id)
			seen[id] = true
			if _, ok := m.wiz.windows[id]; !ok {
				if bw := models.DefaultContextWindow(id); bw > 0 {
					m.wiz.windows[id] = bw
				}
			}
		}
	}
	if len(unlisted) > 0 {
		m.allModels = append(unlisted, m.allModels...)
	}

	m.modelFilter = ""
	m.renderModels()
}

// singleListTool reports whether the picker is single-select: the tool has no
// model menu (Codex), so checking a new id replaces the previous one.
func (m *model) singleListTool() bool {
	return m.tool == nil || m.tool.ModelMenu == ""
}

// toggleModel adds or removes a model id from the curated picker list, keeping
// first-seen order so the list reads in the order the user checked things off.
// A single-select tool keeps exactly one checked id.
func (m *model) toggleModel(id string) {
	for i, sel := range m.wiz.models {
		if sel == id {
			m.wiz.models = append(m.wiz.models[:i:i], m.wiz.models[i+1:]...)
			if m.wiz.model == id {
				if len(m.wiz.models) > 0 {
					m.wiz.model = m.wiz.models[0]
					m.setStatus(statusInfo, fmt.Sprintf("default model switched to %s", m.wiz.model))
				} else {
					m.wiz.model = ""
				}
			}
			return
		}
	}
	if m.singleListTool() {
		m.wiz.models = []string{id}
		m.wiz.model = id
		return
	}
	m.wiz.models = append(m.wiz.models[:len(m.wiz.models):len(m.wiz.models)], id)
	if m.wiz.model == "" {
		m.wiz.model = id
	}
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

// landingValue is the row the picker should open on: the first checked id, else
// the stored model, else the first fetched model.
func (m *model) landingValue() string {
	if len(m.wiz.models) > 0 {
		return m.wiz.models[0]
	}
	if m.wiz.model != "" {
		return m.wiz.model
	}
	if len(m.allModels) > 0 {
		return m.allModels[0]
	}
	return ""
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

// modelRowTitle prefixes a model row with a two-state mark: "[✓]" when the id is
// in the curated list, "[ ]" otherwise. The marks are the same width so ids line up.
func modelRowTitle(id string, checked bool, window int) string {
	windowText := "context unknown"
	if window > 0 {
		windowText = fmt.Sprintf("context %d", window)
	}
	if checked {
		return "[✓] " + id + " · " + windowText
	}
	return "[ ] " + id + " · " + windowText
}

func modelRowDescription(id string, source catalog.WindowSource) string {
	if source == catalog.WindowManual {
		return "manual"
	}
	if source == catalog.WindowBuiltin {
		if !models.IsKnownBuiltin(id) {
			return "builtin (default 500K fallback)"
		}
		return "builtin"
	}
	return "unknown"
}

// renderModels rebuilds the picker rows for the current query (echoed in the title).
// The list holds model rows only; finish, manual entry, and back live on keys.
func (m *model) renderModels() {
	ids := filterModels(m.allModels, m.modelFilter)
	items := make([]list.Item, 0, len(ids))
	windowSources := m.modelWindowSources()
	for _, id := range ids {
		checked := m.modelSelected(id)
		items = append(items, item{title: modelRowTitle(id, checked, m.wiz.windows[id]),
			desc: modelRowDescription(id, windowSources[id]), value: id, active: checked})
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
	multi := m.tool != nil && m.tool.ModelMenu != ""
	switch n := len(m.wiz.models); {
	case !multi:
		title += " · single model only"
	case len(m.allModels) == 0:
	case n == 0 && m.modelFilter == "":
		title += fmt.Sprintf(" · none checked — all %d will be registered", len(m.allModels))
	default:
		title += fmt.Sprintf(" · %d of %d selected", n, len(m.allModels))
	}
	if m.modelFilter != "" {
		title += fmt.Sprintf(" · search: %s (%d matches)", m.modelFilter, len(ids))
	}
	m.list.Title = title

	keys := []key.Binding{keyFinish}
	if !multi {
		keys = []key.Binding{keyChoose}
	}
	if m.modelFilter != "" {
		keys = append(keys, keyClearFilter)
	} else {
		keys = append(keys, keyEsc)
	}
	keys = append(keys, keyWindow)
	if multi {
		keys = append(keys, keyToggle, keyToggleAll, keyManual)
	}
	keys = append(keys, keyFilter, keyRefresh)
	m.setFooterKeys(keys...)
	m.setDelegate(themedCompactDelegate())
}

func (m *model) modelWindowSources() map[string]catalog.WindowSource {
	sources := map[string]catalog.WindowSource{}
	if m.cat != nil && m.tool != nil {
		if p, found, err := m.cat.ProviderByURL(m.tool.ResolveEndpoint(m.wiz.endpoint)); err == nil && found {
			if ms, err := m.cat.ModelsForProvider(p.ID); err == nil {
				for _, stored := range ms {
					sources[stored.Slug] = stored.ContextWindowSource
				}
			}
		}
	}
	for slug := range m.wiz.windows {
		if m.wiz.manualWindows != nil && m.wiz.manualWindows[slug] {
			sources[slug] = catalog.WindowManual
		} else if _, ok := sources[slug]; !ok {
			sources[slug] = catalog.WindowBuiltin
		}
	}
	for _, slug := range m.allModels {
		if m.wiz.manualWindows != nil && m.wiz.manualWindows[slug] {
			sources[slug] = catalog.WindowManual
		} else if _, ok := sources[slug]; !ok {
			sources[slug] = catalog.WindowBuiltin
		}
	}
	return sources
}

// updatePickModel drives the picker: printable keys search, space/x checks the
// highlighted model, ctrl+a toggles all models, enter finishes, m opens manual
// entry, esc returns. The list contains model rows only.
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
		return m.finishPicker()
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
		// m and x are shortcuts only before a search has started; once a query is
		// underway they stay part of the filter, so ids containing those letters match.
		if m.modelFilter == "" && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'w', 'W':
				return m.editModelWindow()
			case 'm', 'M':
				m.clearStatus()
				return m.startManualModels()
			case 'x', 'X':
				return m.toggleHighlighted()
			}
		}
		m.modelFilter += string(msg.Runes)
		m.renderModels()
		return m, nil
	case tea.KeyCtrlA:
		// Ctrl+A toggles selection for all models (or all filtered matches).
		if m.singleListTool() {
			title := "this tool"
			if m.tool != nil {
				title = m.tool.Title
			}
			m.setStatus(statusInfo, title+" can't be given a model list — press enter to pick one model")
			return m, nil
		}
		if len(m.allModels) == 0 {
			return m, nil
		}
		cursor := m.list.Index()
		m.toggleAllModels()
		m.renderModels()
		m.list.Select(cursor)
		return m, nil
	case tea.KeySpace:
		return m.toggleHighlighted()
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// toggleHighlighted checks or unchecks the model under the cursor. A single-select
// tool (Codex) refuses the toggle: its config holds one model, chosen with enter.
func (m model) toggleHighlighted() (tea.Model, tea.Cmd) {
	if m.singleListTool() {
		title := "this tool"
		if m.tool != nil {
			title = m.tool.Title
		}
		m.setStatus(statusInfo, title+" can't be given a model list — press enter to pick one model")
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
	return m, nil
}

// finishPicker validates the selection and advances to the review table.
// In single-select mode, the highlighted row is chosen. In multi-select mode,
// at least one model must be checked; zero checked models is blocked with an error.
func (m model) finishPicker() (tea.Model, tea.Cmd) {
	if it, ok := m.list.SelectedItem().(item); ok && !isSentinel(it.value) {
		if m.singleListTool() {
			m.wiz.models = []string{it.value}
			m.wiz.model = it.value
		}
	}
	if len(m.wiz.models) == 0 {
		m.setStatus(statusErr, "at least 1 model must be selected — press space to check models")
		return m, nil
	}
	if m.wiz.model == "" || !m.modelSelected(m.wiz.model) {
		m.wiz.model = m.wiz.models[0]
	}
	m.reviewSource = viewPickModel
	m.view = viewReviewModels
	m.reviewCursor = 0
	m.clearStatus()
	return m, nil
}

// editModelWindow opens the one-field editor for the highlighted model. Empty saves
// unknown (0); only positive whole token counts are accepted.
func (m model) editModelWindow() (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(item)
	if !ok || isSentinel(it.value) {
		return m, nil
	}
	m.editTarget = it.value
	m.editField = ""
	m.view = viewEditField
	m.startInput("context window in tokens (empty = unknown)", false)
	m.input.SetValue("")
	if w, ok := m.wiz.windows[it.value]; ok && w > 0 {
		m.input.SetValue(fmt.Sprint(w))
	} else if m.cat != nil && m.tool != nil {
		if p, found, err := m.cat.ProviderByURL(m.tool.ResolveEndpoint(m.wiz.endpoint)); err == nil && found {
			if ms, err := m.cat.ModelsForProvider(p.ID); err == nil {
				for _, stored := range ms {
					if stored.Slug == it.value && stored.ContextWindow > 0 {
						m.input.SetValue(fmt.Sprint(stored.ContextWindow))
					}
				}
			}
		}
	}
	return m, textinput.Blink
}
