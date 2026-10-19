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
		return []key.Binding{keyConfirmReview, keyAdd, keyEditContext, keyDeleteRow, keyMove, keyEsc}
	}
	return []key.Binding{keyContinue, keyEsc}
}

func (m model) View() string {
	switch m.view {
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
			modelActionRow("├── [ Fetch & Pick Online Models ]", m.formFocus == focusFetch),
			modelActionRow("└── [ Add / Edit Model List ]", m.formFocus == focusManual),
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
		return m.withFooter(body, keyMove, keyNextField, keySelectAction, keyCancel)
	}

	if m.showConfirm {
		return m.confirmDialog()
	}
	out := m.list.View()
	if m.view == viewTools {
		out = banner(m.version) + "\n\n" + out // blank line between the banner and the list title
	}
	if m.view == viewProfiles && m.configSummary != "" {
		out += "\n" + m.renderConfigSummary()
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

func (m model) renderReviewTable() string {
	header := m.wizardHeader()
	sub := promptStyle.Render("Models and context windows:") + "\n\n"

	maxSlugLen := 18
	for _, s := range m.wiz.models {
		if len(s) > maxSlugLen {
			maxSlugLen = len(s)
		}
	}
	if maxSlugLen > 40 {
		maxSlugLen = 40
	}

	windowSources := m.modelWindowSources()

	hdrRow := fmt.Sprintf("  %-3s %-*s  %s", "#", maxSlugLen, "Model ID", "ctx")
	divider := "  " + strings.Repeat("─", maxSlugLen+26)
	lines := []string{hintStyle.Render(hdrRow), hintStyle.Render(divider)}

	if len(m.wiz.models) == 0 {
		lines = append(lines, hintStyle.Render("No models yet — add the first one."))
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
	default:
		addRow := "  ＋ Add model…"
		if m.reviewCursor >= len(m.wiz.models) {
			addRow = promptStyle.Render("❯ ＋ Add model…")
			lines = append(lines, addRow)
			lines = append(lines, hintStyle.Render("enter types a new model id · w/e edits the highlighted row"))
		} else {
			lines = append(lines, hintStyle.Render(addRow))
			lines = append(lines, hintStyle.Render("enter confirms · a adds a model · w/e edits context · d removes"),
				hintStyle.Render("Context windows are derived when a model is added."))
		}
	}

	body := header + sub + strings.Join(lines, "\n")
	if m.addingModel {
		return m.withFooter(body, keyAddModel, keyCancelAdd)
	}
	if m.editingCtx {
		return m.withFooter(body, keySaveAction, keyCancel)
	}
	keys := []key.Binding{keyConfirmReview, keyAdd, keyEditContext, keyDeleteRow, keyMove, keyEsc}
	if m.reviewCursor >= len(m.wiz.models) {
		keys[0] = keyAddModel
	}
	return m.withFooter(body, keys...)
}
