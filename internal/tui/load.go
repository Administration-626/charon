package tui

import (
	"fmt"
	"strings"

	"charon/internal/catalog"
	"charon/internal/tools"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
)

// aggregationNote summarizes the endpoints a multi-endpoint tool registers
// beyond the binding's own, or "" when there is nothing extra to show: the tool
// holds one endpoint, the catalog cannot be read, or only one endpoint qualifies.
func aggregationNote(t *tools.Tool, cat *catalog.Catalog) string {
	if t == nil || !t.MultiEndpoint || t.ModelMenu == "" || cat == nil {
		return ""
	}
	endpoints, models, err := cat.AggregateCounts(t.Name)
	if err != nil || endpoints <= 1 {
		return ""
	}
	return fmt.Sprintf("%s: %d endpoints (%d models)", t.ModelMenu, endpoints, models)
}

func (m *model) loadTools() {
	var items []list.Item
	selectedIndex := 0
	for i, t := range m.allTools {
		active := "—"
		if b, found, err := m.cat.Active(t.Name); err == nil && found {
			active = b.Name
		} else if err != nil {
			active = "unavailable"
		}
		desc := fmt.Sprintf("last confirmed: %s · live: not detected", active)
		if t.Detected != nil && t.Detected() {
			info, _ := t.Describe()
			segs := []string{"last confirmed: " + active, "live: " + info.AuthMode}
			for _, s := range []string{info.Endpoint, info.Model} {
				if s = strings.TrimSpace(s); s != "" {
					segs = append(segs, s)
				}
			}
			desc = strings.Join(segs, " · ")
			// The note only shows for a detected tool: it describes what a switch
			// would write, which is pointless next to "not detected".
			if note := aggregationNote(t, m.cat); note != "" {
				desc += " · " + note
			}
		}
		items = append(items, item{title: t.Title, desc: desc, value: t.Name})
		if m.tool != nil && t.Name == m.tool.Name {
			selectedIndex = i
		}
	}
	items = append(items, item{value: sepSentinel},
		item{title: "Model Library", desc: "local models and context windows", value: libSentinel})
	m.list.SetItems(items)
	m.list.Select(selectedIndex)
	m.list.Title = "Charon — select a tool"
	m.setFooterKeys(keyOpen, keyQuit)
	m.setDelegate(themedDelegate()) // two-line rows show each tool's status
}

func (m *model) loadModels(selectID string, fallbackIndex ...int) {
	var items []list.Item
	models, _ := m.cat.Models()
	providerRows, _ := m.cat.Providers()
	providers := map[string]string{}
	for _, p := range providerRows {
		providers[p.ID] = p.BaseURL
	}
	visible := models
	if m.modelFilter != "" {
		visible = nil
		for _, stored := range models {
			if modelMatchesFilter(stored, providers[stored.ProviderID], m.modelFilter) {
				visible = append(visible, stored)
			}
		}
	}
	selectedIndex := -1
	for i, stored := range visible {
		if selectID != "" && (stored.ID == selectID || stored.Slug == selectID) {
			selectedIndex = i
		}
		window := "unknown"
		if stored.ContextWindow > 0 {
			window = formatTokens(stored.ContextWindow)
		}
		source := "unknown"
		if stored.ContextWindowSource == catalog.WindowManual || stored.ContextWindowSource == catalog.WindowBuiltin {
			source = string(stored.ContextWindowSource)
		}
		items = append(items, item{
			title: stored.Slug,
			cells: []string{stored.Slug, providers[stored.ProviderID], window, source},
			value: stored.ID,
		})
	}
	switch {
	case len(models) == 0:
		items = append(items, item{title: "No models yet", value: noneSentinel})
	case len(visible) == 0:
		items = append(items, item{title: "No match — esc clears the search", value: noneSentinel})
	default:
		// The column labels lead the table; the cursor skips them (see skipSeparators).
		items = append([]list.Item{item{cells: modelTableHeader, value: hdrSentinel}}, items...)
	}
	items = append(items, item{title: "＋ Add model…", value: addSentinel})

	if selectedIndex >= 0 && len(visible) > 0 {
		selectedIndex++ // the header row leads the table
	}
	if selectedIndex < 0 {
		switch {
		case len(fallbackIndex) > 0:
			selectedIndex = fallbackIndex[0]
		case len(visible) > 0:
			selectedIndex = 1 // first data row, under the column labels
		default:
			selectedIndex = 0
		}
	}
	if selectedIndex >= len(items) {
		selectedIndex = len(items) - 1
	}
	if selectedIndex < 0 {
		selectedIndex = 0
	}
	m.list.SetItems(items)
	m.list.Select(selectedIndex)
	title := "Model Library — local"
	if m.modelFilter != "" {
		title += fmt.Sprintf(" · search: %s (%d of %d)", m.modelFilter, len(visible), len(models))
	}
	m.list.Title = title
	keys := []key.Binding{keyOpen, keyEditContext, keyEffort, keyDelete, keyFilter}
	if m.modelFilter != "" {
		keys = append(keys, keyClearFilter)
	} else {
		keys = append(keys, keyBack)
	}
	m.setFooterKeys(keys...)
	m.setDelegate(themedTableDelegate())
}

// modelMatchesFilter reports whether a model row matches the library search.
// The query matches the slug or the endpoint that offers it (host or full URL),
// case-insensitively, as a plain substring — a query like "one.example" has to
// hit the endpoint column the way it reads.
func modelMatchesFilter(stored catalog.Model, endpoint, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(stored.Slug), q) ||
		strings.Contains(strings.ToLower(endpoint), q)
}

func (m *model) editModelWindowByID(id string) error {
	stored, err := m.cat.Model(id)
	if err != nil {
		return err
	}
	m.editingModel = stored
	if p, err := m.cat.Provider(stored.ProviderID); err == nil {
		m.modelEditEndpoint = p.BaseURL
	}
	m.openWindowPresets(false)
	return nil
}

func formatThinkingLevelMap(levels map[string]*string, effort string) string {
	if len(levels) == 0 {
		return effort
	}
	var out []string
	for _, level := range []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"} {
		value, ok := levels[level]
		if !ok {
			continue
		}
		if value == nil {
			out = append(out, level+"=off")
		} else {
			out = append(out, level+"="+*value)
		}
	}
	return strings.Join(out, ",")
}

func parseThinkingLevelMap(raw string) (map[string]*string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", nil
	}
	if !strings.Contains(raw, "=") {
		return nil, strings.ToLower(raw), nil
	}
	levels := map[string]*string{}
	var fallback string
	valid := map[string]bool{"off": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	for _, part := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !valid[key] || value == "" {
			return nil, "", fmt.Errorf("use level=value pairs, e.g. low=low,medium=medium; unsupported levels are omitted")
		}
		if value == "off" {
			levels[key] = nil
		} else {
			value = strings.ToLower(strings.TrimSpace(value))
			levels[key] = &value
			if key == "medium" {
				fallback = value
			}
		}
	}
	return levels, fallback, nil
}

func (m *model) editModelEffortByID(id string) error {
	stored, err := m.cat.Model(id)
	if err != nil {
		return err
	}
	m.editingModel = stored
	if p, err := m.cat.Provider(stored.ProviderID); err == nil {
		m.modelEditEndpoint = p.BaseURL
	}
	m.startInput("thinking map (level=value, level=off; empty clears)", false)
	m.input.SetValue(formatThinkingLevelMap(stored.ThinkingLevelMap, stored.Effort))
	return nil
}

// loadDuplicateTools lists the duplicate destinations: this tool first, then every
// other tool, marking whether each is installed on this machine.
func (m *model) loadDuplicateTools() {
	var items []list.Item
	items = append(items, item{
		title: "This tool: " + m.tool.Title,
		desc:  "duplicate within " + m.tool.Title + " — shares the endpoint and key",
		value: m.tool.Name,
	})
	for _, t := range m.allTools {
		if t.Name == m.tool.Name {
			continue
		}
		desc := "not installed — copy will be saved without switching"
		if t.Detected != nil && t.Detected() {
			desc = "installed"
		}
		items = append(items, item{title: t.Title, desc: desc, value: t.Name})
	}
	m.list.SetItems(items)
	m.list.Select(0)
	m.list.Title = "Duplicate " + m.dupSource + " to"
	m.setFooterKeys(keyChoose, keyBack)
	m.setDelegate(themedDelegate())
}

// loadProfiles rebuilds the binding list for the current tool. selectName, if
// non-empty, is the row the cursor should land on (e.g. the binding just
// edited or cloned); otherwise the cursor defaults to the active binding.
// This keeps an edit or clone from silently relocating the cursor onto
// whatever happens to be active — only an explicit switch should do that.
func (m *model) loadProfiles(selectName string) {
	m.configSummary = ""
	if m.tool.Describe != nil {
		if info, err := m.tool.Describe(); err == nil {
			m.configSummary = configSummary(info)
		} else {
			m.configSummary = "On-disk config could not be read."
		}
	}
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
		items = append(items, item{title: "＋ Add new binding…", value: addSentinel})
		if len(saved) > 0 {
			items = append(items, item{value: sepSentinel})
			// The column labels lead the table; the cursor skips them (see skipSeparators).
			items = append(items, item{cells: bindingTableHeader, value: hdrSentinel})
		}
	}

	for _, b := range saved {
		title := b.Name
		isActive := b.Name == active
		if isActive {
			title = "✓ " + title
		}
		if b.Hidden {
			// Kept in charon, left out of the tool's endpoint list.
			title = "~ " + title
		}
		cells, drift := m.bindingRow(b.Name)
		cells[0] = title
		items = append(items, item{cells: cells, value: b.Name, active: isActive, hidden: b.Hidden})
		if drift != "" {
			// The comparison sits on its own inert row under the binding, so every
			// table row stays exactly one line and pagination stays honest.
			items = append(items, item{title: drift, value: driftSentinel + b.Name})
		}
	}

	selectedIndex := 0
	if target != "" {
		for i, raw := range items {
			if it, ok := raw.(item); ok && it.value == target {
				selectedIndex = i
				break
			}
		}
	}
	m.list.SetItems(items)
	m.list.Select(selectedIndex)
	m.list.Title = m.tool.Title + " bindings (✓ active)"
	if m.tool.MultiEndpoint {
		m.list.Title = m.tool.Title + " bindings (✓ active · ~ hidden)"
	}
	keys := []key.Binding{keySwitch, keyAdd, keyReapply, keyEdit, keyDuplicateRow}
	if m.tool.MultiEndpoint {
		// Only a tool that holds several endpoints has a list to hide from.
		keys = append(keys, keyHide)
	}
	keys = append(keys, keyDelete, keyRefresh, keyBack)
	m.setFooterKeys(keys...)
	m.setDelegate(themedTableDelegate())
	m.resize()
	if len(saved) == 0 && m.status == "" && m.tool.ApplyAuth != nil {
		m.setStatus(statusInfo, `No bindings yet — press enter on "Add new binding" or press 'a' to create one.`)
	}
}

// nonEmpty is s, or fallback when s is blank.
func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// infoDrifts reports whether a tool's on-disk config disagrees with the saved
// binding values (endpoint or default model). Whitespace around the endpoint is
// ignored; a missing live model reads as drift too, since charon always saves
// one.
func infoDrifts(info tools.Info, savedURL, savedSlug string) bool {
	liveURL, liveModel := strings.TrimSpace(info.Endpoint), strings.TrimSpace(info.Model)
	if liveURL != "" && !sameEndpoint(liveURL, savedURL) {
		return true
	}
	return liveModel != "" && liveModel != savedSlug
}

// sameEndpoint compares endpoints ignoring a trailing slash and a trailing
// /v1: some tools normalize the base URL on write (Claude drops /v1 because it
// requests /v1/messages itself), so charon's own output would otherwise read
// back as drift.
func sameEndpoint(a, b string) bool {
	return trimEndpoint(a) == trimEndpoint(b)
}

func trimEndpoint(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	s = strings.TrimSuffix(s, "/v1")
	return strings.TrimRight(s, "/")
}

// bindingRow splits a binding into the bindings-table columns: name, endpoint,
// default model, and how many extra models its picker offers. The request
// protocol a Pi binding stores is deliberately not a column — a wrong protocol
// surfaces as an access failure inside the tool's own harness, and the row
// stays scannable without it (the edit form is where the protocol is chosen).
//
// For the active binding, drift is returned separately: when the on-disk
// config disagrees with the saved values (say, the user edited the config by
// hand), the caller shows the saved/live comparison under the row. When they
// agree, nothing is returned — the config summary below the list already
// shows the live endpoint and model.
func (m *model) bindingRow(name string) (cells []string, drift string) {
	b, found, err := m.cat.BindingByName(m.tool.Name, name)
	if err != nil || !found {
		return []string{name, "unknown", "unknown", ""}, ""
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
	if url == "" {
		url = "default endpoint"
	}
	if slug == "" {
		slug = "no model"
	}
	more := ""
	if extra > 0 {
		more = fmt.Sprintf("+%d", extra)
	}
	if active, ok, err := m.cat.Active(m.tool.Name); err == nil && ok && active.Name == name && m.tool.Describe != nil {
		if info, err := m.tool.Describe(); err == nil && infoDrifts(info, url, slug) {
			drift = "saved: " + url + " · " + slug +
				" · live: " + nonEmpty(info.Endpoint, "default") + " · " + nonEmpty(info.Model, "unknown")
		}
	}
	return []string{name, url, slug, more}, drift
}
