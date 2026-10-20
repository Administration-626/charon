package tui

import (
	"fmt"
	"strings"

	"charon/internal/catalog"
	"charon/internal/models"
	"charon/internal/secret"
	"charon/internal/tools"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

func (m model) endpointHint(endpoint string) string {
	if m.tool != nil && m.tool.Name == "pi" {
		switch m.wiz.piAPI {
		case "google-generative-ai":
			return hintStyle.Render("Gemini: use the provider's documented endpoint; type model IDs manually.")
		case "anthropic-messages":
			return ""
		}
	}
	if tools.EndpointHasClaudeV1(m.tool, endpoint) {
		return warnStyle.Render("Warning: this URL includes /v1; Claude Code requests /v1/messages. Confirm the gateway endpoint.")
	}
	if !tools.EndpointNeedsV1Hint(m.tool, endpoint) {
		return ""
	}
	return warnStyle.Render("Warning: no /v1; discovery adds /v1/models. Confirm the gateway endpoint.")
}

// modelActionRow renders one of the tree-indented buttons under the Model Slug field,
// keeping the focused and unfocused variants aligned to the same column.
func modelActionRow(label string, focused bool) string {
	const indent = "                   "
	if focused {
		return promptStyle.Render(indent[:len(indent)-2] + "▌ " + label)
	}
	return hintStyle.Render(indent + label)
}

// statusRender styles a status line for the level (glyph-prefixed); "" for an empty message.
func statusRender(level statusLevel, msg string) string {
	if msg == "" {
		return ""
	}
	switch level {
	case statusOK:
		return successStyle.Render("✓ " + msg)
	case statusErr:
		return errorStyle.Render("✗ " + msg)
	case statusWarn:
		return warnStyle.Render("! " + msg)
	default:
		return statusStyle.Render(msg)
	}
}

// helpKeys adapts a binding list to bubbles' help renderer, so every screen draws its
// shortcuts with the same component instead of hand-written prose.
type helpKeys []key.Binding

func (h helpKeys) ShortHelp() []key.Binding  { return h }
func (h helpKeys) FullHelp() [][]key.Binding { return [][]key.Binding{h} }

// helpLine renders that legend for one screen, truncated to width.
func (m model) helpLine(width int, bindings ...key.Binding) string {
	if len(bindings) == 0 {
		return ""
	}
	h := m.list.Help
	h.Width = width
	return h.View(helpKeys(bindings))
}

// withFooter pins the chrome to the bottom of the terminal: the status line, then the
// key legend on the last row. Content shorter than the screen is padded above, so the
// legend sits in the same place on every screen and content that grows eats the gap
// instead of pushing the legend down.
func (m model) withFooter(body string, bindings ...key.Binding) string {
	if !m.showHelp {
		// ? is universal; keep it at the end of every legend so the full help
		// sheet is discoverable without spelling it out per screen.
		bindings = append(append([]key.Binding{}, bindings...), keyHelp)
	}
	footer := statusRender(m.statusLvl, m.status) + "\n" + m.helpLine(m.width, bindings...)
	pad := m.height - footerRows - lipgloss.Height(body)
	if pad < 0 {
		pad = 0
	}
	return body + strings.Repeat("\n", pad+1) + footer
}

// stepHelp is the legend for the current input step: exactly the keys that screen
// answers to, so no step has to spell its shortcuts out by hand.
func (m model) stepHelp() []key.Binding {
	switch m.view {
	case viewAddName:
		return []key.Binding{keySaveAction, keyEsc}
	case viewEditField:
		return []key.Binding{keySaveAction, keyCancel}
	case viewDupName:
		return []key.Binding{keyDuplicate, keyCancel}
	case viewReviewModels:
		return []key.Binding{keyConfirmReview, keyAdd, keyFetch, keyEditRow, keyEditContext, keyDeleteRow, keyMove, keyEsc}
	}
	return []key.Binding{keyContinue, keyEsc}
}

// helpBindings lists the keys the current screen answers to, mirroring what its
// footer passes to withFooter. Screens with explicit footers (the form, the model
// list) spell them out here too.
func (m model) helpBindings() []key.Binding {
	switch m.view {
	case viewEditForm:
		return []key.Binding{keyMove, keyNextField, keySelectAction, keySave, keyCancel}
	case viewReviewModels:
		switch {
		case m.addingModel:
			return []key.Binding{keyAddModel, keyCancelAdd}
		case m.editingCtx, m.editingID:
			return []key.Binding{keySaveAction, keyCancel}
		}
		keys := []key.Binding{keyConfirmReview, keyAdd, keyFetch, keyEditRow, keyEditContext, keyDeleteRow, keyMove}
		if m.reviewCursor >= len(m.wiz.models) {
			keys[0] = keyAddModel
		}
		return keys
	case viewBindingDetail:
		return []key.Binding{keyEdit, keyEsc}
	case viewProfiles:
		// The short legend drops the niche keys; the sheet still lists them all.
		keys := []key.Binding{keySwitch, keyAdd, keyEdit, keyDetails, keyDuplicateRow}
		if m.tool != nil && m.tool.MultiEndpoint {
			keys = append(keys, keyHide)
		}
		return append(keys, keyDelete, keyReapply, keyRefresh, keyBack)
	case viewAddName, viewEditField, viewDupName, viewAddEndpoint, viewAddKey:
		return m.stepHelp()
	default:
		return m.footerKeys
	}
}

// renderHelp is the ? sheet: every shortcut the current screen accepts, plus the
// two globals. Any key closes it (handled in Update).
func (m model) renderHelp() string {
	lines := []string{"", titleStyle.Render("Keyboard shortcuts"), ""}
	seen := map[string]bool{}
	add := func(bindings ...key.Binding) {
		for _, b := range bindings {
			h := b.Help()
			if h.Key == "" || seen[h.Key] {
				continue
			}
			seen[h.Key] = true
			lines = append(lines, fmt.Sprintf("  %-12s %s", h.Key, h.Desc))
		}
	}
	add(m.helpBindings()...)
	add(keyHelp, keyForceQuit)
	return m.withFooter(strings.Join(lines, "\n"), keyCloseHelp)
}

func (m model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}
	switch m.view {
	case viewBindingDetail:
		return m.renderBindingDetailScreen()
	case viewReviewModels:
		return m.renderReviewTable()
	case viewWindowPresets:
		return m.withFooter(m.list.View(), m.footerKeys...)
	case viewFetching:
		return m.withFooter(m.wizardHeader() +
			promptStyle.Render(m.spinner.View()+m.loadingMsg) +
			"\n\n" + hintStyle.Render("fetching models from "+m.wiz.endpoint))
	case viewModelEndpoint, viewModelSlug, viewModelWindow, viewModelEffort:
		body := "\n" + titleStyle.Render("Model Library — local") + "\n\n" +
			promptStyle.Render(m.prompt()) + "\n\n  " + m.input.View()
		if m.view == viewModelEndpoint && m.editingModel.ID != "" {
			body += "\n\n" + hintStyle.Render("Endpoint: "+m.modelEditEndpoint)
		}
		if m.view == viewModelEndpoint {
			if hint := m.endpointHint(m.input.Value()); hint != "" {
				body += "\n\n" + hint
			}
		}
		return m.withFooter(body, m.stepHelp()...)
	case viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField:
		body := m.wizardHeader() +
			promptStyle.Render(m.prompt()) +
			"\n\n  " + m.input.View()
		if m.view == viewAddEndpoint || (m.view == viewEditField && m.editField == fieldURL) {
			if hint := m.endpointHint(m.input.Value()); hint != "" {
				body += "\n\n" + hint
			}
		}
		return m.withFooter(body, m.stepHelp()...)
	case viewEditForm:
		title := m.tool.Title + " · Edit Binding"
		if !m.wiz.edit {
			title = m.tool.Title + " · New Binding"
		}
		header := "\n" + titleStyle.Render(title) + "\n\n"

		labels := []string{"Name         ", "Endpoint     ", "API Key/Token"}
		var formLines []string

		for i := 0; i < formInputCount; i++ {
			if m.formFocus == i {
				// Focused Row: Bold accent bar, bold label, clear input
				bar := promptStyle.Render("▌ ")
				labelStr := promptStyle.Render(labels[i] + " : ")
				inputStr := m.formInputs[i].View()
				formLines = append(formLines, bar+labelStr+inputStr)
			} else {
				// Unfocused Row: Muted, low contrast
				bar := "  "
				labelStr := hintStyle.Render(labels[i] + " : ")
				inputVal := m.formInputs[i].Value()
				if inputVal == "" {
					inputVal = m.formInputs[i].Placeholder
				} else if i == focusToken { // Partial masking for API Key
					inputVal = secret.Mask(inputVal)
				}
				inputStr := hintStyle.Render(inputVal)
				formLines = append(formLines, bar+labelStr+inputStr)
			}
		}

		if m.tool.Name == "pi" {
			label := piAPILabel(m.wiz.piAPI)
			row := "Endpoint Type: [ " + label + " ]"
			if m.formFocus == focusPiAPI {
				formLines = append(formLines, promptStyle.Render("▌ "+row))
			} else {
				formLines = append(formLines, "  "+hintStyle.Render("Endpoint Type: [ ")+promptStyle.Render(label)+hintStyle.Render(" ]"))
			}
			if m.piAPIOpen {
				for i, option := range piAPIOptions {
					mark := "[ ] "
					if option.api == m.wiz.piAPI {
						mark = "[✓] "
					}
					row := mark + option.label
					if i == m.piAPICursor {
						row = promptStyle.Render("  > " + row)
					} else if option.api == m.wiz.piAPI {
						row = promptStyle.Render("    " + row)
					} else {
						row = hintStyle.Render("    " + row)
					}
					formLines = append(formLines, row)
				}
			}
		}

		var modelDisplay string
		if len(m.wiz.models) > 1 {
			if note := m.pickerNote(); note != "" {
				modelDisplay = promptStyle.Render(note)
			} else {
				modelDisplay = promptStyle.Render(fmt.Sprintf("%d models configured", len(m.wiz.models)))
			}
		} else {
			modelVal := m.wiz.model
			if modelVal == "" && len(m.wiz.models) == 1 {
				modelVal = m.wiz.models[0]
			}
			if modelVal == "" {
				modelDisplay = hintStyle.Render("(none configured — choose below)")
			} else {
				modelDisplay = promptStyle.Render(modelVal)
			}
		}
		formLines = append(formLines,
			"  "+hintStyle.Render("Models       : ")+modelDisplay,
			modelActionRow("└── [ Models… ]", m.formFocus == focusModels),
		)

		saveBtn := hintStyle.Render("  [ Save ]")
		if m.formFocus == focusSave {
			saveBtn = promptStyle.Render("▌ [ Save ]")
		}
		cancelBtn := hintStyle.Render("  [ Cancel ]")
		if m.formFocus == focusCancel {
			cancelBtn = promptStyle.Render("▌ [ Cancel ]")
		}

		body := header + strings.Join(formLines, "\n") + "\n  " + saveBtn + "\n  " + cancelBtn
		if hint := m.endpointHint(m.formInputs[focusURL].Value()); hint != "" {
			body += "\n\n" + hint
		}
		return m.withFooter(body, keyMove, keyNextField, keySelectAction, keySave, keyCancel)
	}

	if m.showConfirm {
		return m.confirmDialog()
	}
	out := m.list.View()
	if m.view == viewTools {
		out = banner(m.version) + "\n\n" + out // blank line between the banner and the list title
	}
	if m.view == viewProfiles {
		if m.wideDetail() {
			out = lipgloss.JoinHorizontal(lipgloss.Top, out, m.renderBindingDetailPane())
		} else if m.configSummary != "" {
			out += "\n" + m.renderConfigSummary()
		}
	}
	return m.withFooter(out, m.footerKeys...)
}

// configSummary shows only observed configuration, not catalog predictions or
// the state of a running tool session. No credentials are included.
func configSummary(info tools.Info) string {
	endpoint, auth := info.Endpoint, info.AuthMode
	if endpoint == "" {
		endpoint = "unknown"
	}
	if auth == "" {
		auth = "unknown"
	}
	model, effort := info.Model, info.Effort
	if model == "" {
		model = "unknown"
	}
	if effort == "" {
		effort = "unknown"
	}
	return fmt.Sprintf("On-disk config (Ctrl+R refresh)\nEndpoint: %s · auth: %s\nDefault model: %s\nContext window: %s tokens\nMax output (maxTokens): %s tokens\nReasoning effort: %s\nUnknown = not recorded/read; running sessions may differ.",
		endpoint, auth, model, formatTokens(info.ContextWindow), formatTokens(info.MaxTokens), effort)
}

func (m model) renderConfigSummary() string {
	return hintStyle.Width(max(1, m.width)).Render(m.configSummary)
}

// The binding page splits into a list and a read-only detail pane once the
// terminal is wide enough for both. Narrower terminals keep the full-width list
// and open the same detail with v.
const (
	wideDetailMinWidth = 110
	detailGap          = 2
	detailTopPad       = 4
	detailMinListWidth = 56
	detailMaxListWidth = 80
	detailLabelWidth   = 11
)

func (m model) wideDetail() bool {
	return m.view == viewProfiles && m.width >= wideDetailMinWidth
}

func (m model) detailListWidth() int {
	w := m.width * 55 / 100
	if w < detailMinListWidth {
		w = detailMinListWidth
	}
	if w > detailMaxListWidth {
		w = detailMaxListWidth
	}
	return w
}

func (m model) detailPaneWidth() int {
	w := m.width - m.detailListWidth() - detailGap
	if w < 20 {
		w = 20
	}
	return w
}

// highlightedBindingName returns the binding under the cursor, or false when the
// cursor is on a sentinel row.
func (m model) highlightedBindingName() (string, bool) {
	it, ok := m.list.SelectedItem().(item)
	if !ok || isSentinel(it.value) {
		return "", false
	}
	return it.value, true
}

// hardWrap breaks a value into chunks that fit width display columns. It
// prefers spaces so summaries wrap at words; a long unbroken value such as an
// endpoint or a CJK name is cut. Width is measured in terminal columns, so a
// wide rune counts as two.
func hardWrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	line, lineWidth := "", 0
	flush := func() {
		if line != "" {
			out = append(out, line)
			line, lineWidth = "", 0
		}
	}
	for _, word := range words {
		wordWidth := lipgloss.Width(word)
		if lineWidth > 0 && lineWidth+1+wordWidth > width {
			flush()
		}
		if wordWidth <= width {
			if lineWidth > 0 {
				line += " "
				lineWidth++
			}
			line += word
			lineWidth += wordWidth
			continue
		}
		// The token alone is wider than a line: hard-cut it, keep the last
		// piece as the current line so the next word can still join it.
		flush()
		pieces := hardCut(word, width)
		out = append(out, pieces[:len(pieces)-1]...)
		line = pieces[len(pieces)-1]
		lineWidth = lipgloss.Width(line)
	}
	flush()
	return out
}

// hardCut splits s into pieces no wider than width display columns, cutting at
// rune boundaries. A single rune wider than width is emitted on its own.
func hardCut(s string, width int) []string {
	var out []string
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w > 0 && w+rw > width {
			out = append(out, b.String())
			b.Reset()
			w = 0
		}
		b.WriteRune(r)
		w += rw
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// detailField renders a label and a value, continuing wrapped values under the
// value column.
func detailField(label, value string, width int) []string {
	valueWidth := width - detailLabelWidth
	if valueWidth < 8 {
		valueWidth = 8
	}
	parts := hardWrap(nonEmpty(value, "—"), valueWidth)
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		prefix := strings.Repeat(" ", detailLabelWidth)
		if i == 0 {
			prefix = hintStyle.Render(fmt.Sprintf("%-*s", detailLabelWidth, label))
		}
		out = append(out, prefix+part)
	}
	return out
}

// bindingDetailLines builds the read-only detail for name. maxLines caps the
// output so a long model list cannot push the footer off the screen; 0 means no
// cap.
func (m model) bindingDetailLines(name string, width, maxLines int) []string {
	b, found, err := m.cat.BindingByName(m.tool.Name, name)
	if err != nil || !found {
		return []string{hintStyle.Render("Binding unavailable.")}
	}

	endpoint, key := "", ""
	if cr, err := m.cat.Credential(b.CredentialID); err == nil {
		key = secret.Mask(cr.Key)
		if p, err := m.cat.Provider(cr.ProviderID); err == nil {
			endpoint = p.BaseURL
		}
	}
	if endpoint == "" {
		endpoint = "default endpoint"
	}

	active := false
	if cur, ok, err := m.cat.Active(m.tool.Name); err == nil && ok {
		active = cur.ID == b.ID
	}
	status := "inactive"
	if active {
		status = "✓ active"
	}
	if b.Hidden {
		status += " · ~ hidden (kept in charon)"
	}
	if m.tool.MultiEndpoint {
		// Every visible binding is registered in the same provider list; the
		// active one is only its default.
		switch {
		case b.Hidden:
			status = "hidden · not in the provider list"
		case active:
			status = "✓ default · registered"
		default:
			status = "registered · not default"
		}
	}

	var lines []string
	for _, part := range hardWrap(b.Name, width) {
		lines = append(lines, titleStyle.Render(part))
	}
	for _, part := range hardWrap(m.tool.Title+" binding", width) {
		lines = append(lines, hintStyle.Render(part))
	}
	lines = append(lines, "")
	defaultSlug, _ := m.cat.ModelSlug(b.ModelID)
	lines = append(lines, detailField("Endpoint", endpoint, width)...)
	lines = append(lines, detailField("API key", key, width)...)
	if m.tool.Name == "pi" && b.PiAPI != "" {
		lines = append(lines, detailField("Protocol", piAPILabel(b.PiAPI), width)...)
	}
	lines = append(lines, detailField("Default", defaultSlug, width)...)
	lines = append(lines, detailField("Status", status, width)...)
	modelIDs := b.Models
	if len(modelIDs) == 0 && b.ModelID != "" {
		// Legacy rows can carry only the default id; show it rather than
		// claiming the binding has no models.
		modelIDs = []string{b.ModelID}
	}
	lines = append(lines, "", hintStyle.Render("Models"))
	if len(modelIDs) == 0 {
		lines = append(lines, hintStyle.Render("  (none)"))
	} else {
		type detailModel struct {
			slug   string
			window int
			source string
		}
		var rows []detailModel
		slugWidth := 0
		for _, id := range modelIDs {
			slug, err := m.cat.ModelSlug(id)
			if err != nil {
				continue
			}
			row := detailModel{slug: slug}
			if stored, err := m.cat.Model(id); err == nil {
				row.window = stored.ContextWindow
				switch stored.ContextWindowSource {
				case catalog.WindowManual:
					row.source = "manual"
				case catalog.WindowBuiltin:
					row.source = "builtin"
				}
			}
			if len(slug) > slugWidth {
				slugWidth = len(slug)
			}
			rows = append(rows, row)
		}
		if slugWidth < 8 {
			slugWidth = 8
		}
		if maxWidth := width - 21; slugWidth > maxWidth && maxWidth >= 8 {
			slugWidth = maxWidth
		}
		for _, row := range rows {
			lines = append(lines, fmt.Sprintf("  %s  %8s  %s",
				fitCell(row.slug, slugWidth), formatTokens(row.window), hintStyle.Render(row.source)))
		}
	}

	if active {
		lines = append(lines, m.detailOnDiskLines(width, "On-disk config (active binding)")...)
	} else if !m.tool.MultiEndpoint {
		// Multi-endpoint tools (Pi) register every visible binding's endpoint
		// in the same provider list, so an inactive binding is still written.
		lines = append(lines, "", hintStyle.Width(width).Render("Not active — switch to write these values to the tool."))
	}
	return capDetailLines(lines, maxLines)
}

// detailOnDiskLines renders the cached on-disk summary under a heading.
func (m model) detailOnDiskLines(width int, heading string) []string {
	if m.configSummary == "" {
		return nil
	}
	lines := []string{"", hintStyle.Render(heading)}
	for _, line := range strings.Split(m.configSummary, "\n") {
		if strings.HasPrefix(line, "On-disk config") {
			continue
		}
		for _, part := range hardWrap(line, width) {
			lines = append(lines, hintStyle.Render(part))
		}
	}
	return lines
}

// capDetailLines keeps the detail inside its pane, ending with an ellipsis when
// there is more than fits.
func capDetailLines(lines []string, maxLines int) []string {
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines-1]
		lines = append(lines, hintStyle.Render("…"))
	}
	return lines
}

// renderBindingDetailPane is the right-hand read-only pane in the wide layout.
func (m model) renderBindingDetailPane() string {
	name, ok := m.highlightedBindingName()
	width := m.detailPaneWidth() - detailGap
	if width < 20 {
		width = 20
	}
	maxLines := m.list.Height() - detailTopPad
	if maxLines < 1 {
		maxLines = 1
	}
	var lines []string
	if !ok {
		lines = []string{hintStyle.Render("Select a binding to see its details.")}
		lines = append(lines, m.detailOnDiskLines(width, "On-disk config")...)
		lines = capDetailLines(lines, maxLines)
	} else {
		lines = m.bindingDetailLines(name, width, maxLines)
	}
	padded := make([]string, 0, len(lines)+detailTopPad)
	for i := 0; i < detailTopPad; i++ {
		padded = append(padded, "")
	}
	lines = append(padded, lines...)
	for i, line := range lines {
		lines[i] = strings.Repeat(" ", detailGap) + line
	}
	return strings.Join(lines, "\n")
}

// renderBindingDetailScreen is the narrow-screen detail, opened with v.
func (m model) renderBindingDetailScreen() string {
	width := m.width - 2
	if width < 20 {
		width = 20
	}
	maxLines := m.height - footerRows - 2
	var lines []string
	if name, ok := m.highlightedBindingName(); ok {
		lines = m.bindingDetailLines(name, width, maxLines)
	} else {
		lines = []string{hintStyle.Render("Select a binding first.")}
		lines = append(lines, m.detailOnDiskLines(width, "On-disk config")...)
		lines = capDetailLines(lines, maxLines)
	}
	return m.withFooter("\n"+strings.Join(lines, "\n"), keyEdit, keyEsc)
}

// wizardHeader renders the titled bar for add-flow screens.
func (m model) wizardHeader() string {
	n, total, label := wizardStep(m.view)
	if total == 0 {
		return "\n"
	}
	title := titleStyle.Render(m.tool.Title + " · new binding")
	step := stepStyle.Render(fmt.Sprintf("Step %d of %d · %s", n, total, label))
	return "\n" + title + "\n" + step + "\n\n"
}

func (m model) prompt() string {
	switch m.view {
	case viewModelEndpoint:
		if m.editingModel.ID != "" {
			return "Edit endpoint for " + m.editingModel.Slug + ":"
		}
		return "Model Library — endpoint:"
	case viewModelSlug:
		return "Model Library — model slug:"
	case viewModelWindow:
		if m.editingModel.ID != "" {
			return "Edit context window for " + m.editingModel.Slug + " (empty = unknown):"
		}
		return "Model Library — context window (empty = unknown):"
	case viewModelEffort:
		if m.editingModel.ID != "" {
			return "Edit reasoning effort for " + m.editingModel.Slug + ":"
		}
		return "Model Library — reasoning effort:"
	}
	if m.view == viewEditField {
		if m.editTarget != "" {
			return "Edit context window for " + m.editTarget + " (empty = unknown):"
		}
		switch m.editField {
		case fieldName:
			return "Edit name:"
		case fieldURL:
			return "Edit endpoint:"
		case fieldToken:
			return "Edit API key (hidden):"
		}
	}
	switch m.view {
	case viewAddEndpoint:
		if m.tool.DefaultEndpoint != "" {
			return "endpoint — leave blank for the default (" + m.tool.DefaultEndpoint + "):"
		}
		return "endpoint:"
	case viewAddKey:
		return "API key — input is hidden as you type:"
	case viewAddName:
		return "Name this binding (e.g. work, openrouter-fast):"
	case viewDupName:
		dst := "this tool"
		if m.dupTool != "" && m.dupTool != m.tool.Name {
			if t := m.findTool(m.dupTool); t != nil {
				dst = t.Title
			}
		}
		return "Duplicate " + m.dupSource + " in " + dst + " as:"
	default:
		return ""
	}
}

// confirmDialog renders a centered confirmation box covering the full screen.
func (m model) confirmDialog() string {
	if !m.showConfirm || m.delTarget == "" {
		return ""
	}
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWarn).
		Padding(1, 2).
		Width(min(44, m.width-4))

	content := warnStyle.Render("Delete binding "+m.delTarget+"?") + "\n" +
		"This can't be undone.\n\n" +
		m.helpLine(min(40, m.width-8), keyConfirm, keyCancel)

	dialog := dialogStyle.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func formatTokens(n int) string {
	if n <= 0 {
		return "unknown"
	}
	s := fmt.Sprint(n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

// reviewSlugColWidth is the id column's width in the model list: the longest id
// capped at 40, floored at 18. The inline rename field uses it too, so the input
// lines up with the id it replaces.
func (m model) reviewSlugColWidth() int {
	w := 18
	for _, s := range m.wiz.models {
		if len(s) > w {
			w = len(s)
		}
	}
	if w > 40 {
		w = 40
	}
	return w
}

func (m model) renderReviewTable() string {
	header := m.wizardHeader()
	sub := promptStyle.Render("Models and context windows:") + "\n\n"

	maxSlugLen := m.reviewSlugColWidth()
	windowSources := m.modelWindowSources()

	hdrRow := fmt.Sprintf("  %-3s %-*s  %s", "#", maxSlugLen, "Model ID", "ctx")
	divider := "  " + strings.Repeat("─", maxSlugLen+26)
	lines := []string{hintStyle.Render(hdrRow), hintStyle.Render(divider)}

	if len(m.wiz.models) == 0 {
		lines = append(lines, hintStyle.Render("No models yet — add the first one. a or enter types an id."))
	}
	for i, slug := range m.wiz.models {
		win := m.wiz.windows[slug]
		ctxText := formatTokens(win)
		if win > 0 {
			switch windowSources[slug] {
			case catalog.WindowManual:
				ctxText += " manual"
			case catalog.WindowBuiltin:
				if !models.IsKnownBuiltin(slug) {
					ctxText += " fallback"
				}
			}
		}

		cursorMark := "  "
		numStr := fmt.Sprintf("%d.", i+1)
		displaySlug := slug
		if len(displaySlug) > maxSlugLen {
			displaySlug = displaySlug[:maxSlugLen-1] + "…"
		}
		row := fmt.Sprintf("%s%-3s %-*s  %-18s", cursorMark, numStr, maxSlugLen, displaySlug, ctxText)
		switch {
		case i == m.reviewCursor && m.editingCtx:
			lines = append(lines, fmt.Sprintf("%s%-3s %-*s  %s", cursorMark, numStr, maxSlugLen, displaySlug, m.input.View()))
		case i == m.reviewCursor && m.editingID:
			lines = append(lines, fmt.Sprintf("%s%-3s %s  %-18s", cursorMark, numStr, m.input.View(), ctxText))
		case i == m.reviewCursor:
			lines = append(lines, promptStyle.Render(row))
		default:
			lines = append(lines, row)
		}
	}

	lines = append(lines, hintStyle.Render(divider))
	switch {
	case m.addingModel:
		lines = append(lines, promptStyle.Render("  ＋ ")+m.input.View())
		lines = append(lines, hintStyle.Render("enter adds · esc cancels · commas separate pasted ids"))
	case m.editingCtx:
		lines = append(lines, hintStyle.Render(fmt.Sprintf("ctx for %s — current %s (%s)", m.editTarget,
			formatTokens(m.wiz.windows[m.editTarget]), windowSourceLabel(m.editTarget, windowSources[m.editTarget]))))
		lines = append(lines, hintStyle.Render("presets 32K/64K/128K/200K/256K/400K/500K/1M/2M · enter saves · esc cancels · empty clears"))
	case m.editingID:
		lines = append(lines, hintStyle.Render(fmt.Sprintf("edit %s — the context window follows the new id", m.editTarget)))
		lines = append(lines, hintStyle.Render("enter saves the id and continues to ctx · esc cancels"))
	default:
		addRow := "  ＋ Add model…"
		if m.reviewCursor >= len(m.wiz.models) {
			addRow = promptStyle.Render("❯ ＋ Add model…")
			lines = append(lines, addRow)
			lines = append(lines, hintStyle.Render("enter types a new model id · f fetches online"))
		} else {
			lines = append(lines, hintStyle.Render(addRow))
			lines = append(lines, hintStyle.Render("enter confirms · a adds a model · f fetches · e edits model and ctx · w edits ctx only · d removes"),
				hintStyle.Render("Context windows are derived when a model is added."))
		}
	}

	body := header + sub + strings.Join(lines, "\n")
	if m.addingModel {
		return m.withFooter(body, keyAddModel, keyCancelAdd)
	}
	if m.editingCtx || m.editingID {
		return m.withFooter(body, keySaveAction, keyCancel)
	}
	keys := []key.Binding{keyConfirmReview, keyAdd, keyFetch, keyEditRow, keyEditContext, keyDeleteRow, keyMove, keyEsc}
	if m.reviewCursor >= len(m.wiz.models) {
		keys[0] = keyAddModel
	}
	return m.withFooter(body, keys...)
}
