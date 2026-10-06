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

// modelMenuNote explains where a list of model ids ends up for the selected tool: its
// own in-session menu, or — for a tool that can't be given a list — that only the first
// id takes effect, so nobody types five ids expecting a menu that will never appear.
func (m model) modelMenuNote() string {
	if m.tool == nil {
		return ""
	}
	if m.tool.ModelMenu == "" {
		return m.tool.Title + " can't be given a model list, so only the first id is used." +
			" Add a binding per model to switch between them."
	}
	return "All of them are offered in " + m.tool.Title + "'s own " + m.tool.ModelMenu +
		", so you can switch model without leaving your session."
}

func (m model) endpointHint(endpoint string) string {
	if tools.EndpointHasClaudeV1(m.tool, endpoint) {
		return warnStyle.Render("Warning: this URL includes /v1; Claude Code requests /v1/messages. Confirm the gateway base URL.")
	}
	if !tools.EndpointNeedsV1Hint(m.tool, endpoint) {
		return ""
	}
	return warnStyle.Render("Warning: no /v1; discovery adds /v1/models. Confirm the gateway base URL.")
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
	case viewAddCustomModel:
		return []key.Binding{keyRegister, keyEsc}
	case viewReviewModels:
		return []key.Binding{keyConfirmReview, keyEditContext, keyDeleteRow, keyMove, keyEsc}
	}
	return []key.Binding{keyContinue, keyEsc}
}

func (m model) View() string {
	switch m.view {
	case viewReviewModels:
		return m.renderReviewTable()
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
	case viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField, viewAddCustomModel:
		body := m.wizardHeader() +
			promptStyle.Render(m.prompt()) +
			"\n\n  " + m.input.View()
		if m.view == viewAddEndpoint || (m.view == viewEditField && m.editField == fieldURL) {
			if hint := m.endpointHint(m.input.Value()); hint != "" {
				body += "\n\n" + hint
			}
		}
		if m.view == viewAddCustomModel {
			body += "\n\n" + hintStyle.Render(m.modelMenuNote())
		}
		return m.withFooter(body, m.stepHelp()...)
	case viewEditForm:
		title := m.tool.Title + " · Edit Binding"
		if !m.wiz.edit {
			title = m.tool.Title + " · New Binding"
		}
		header := "\n" + titleStyle.Render(title) + "\n\n"

		labels := []string{"Name         ", "API Base URL ", "API Key/Token"}
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
			modelActionRow("└── [ Type Model IDs Manually ]", m.formFocus == focusManual),
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
	model, effort := info.Model, info.Effort
	if model == "" {
		model = "unknown"
	}
	if effort == "" {
		effort = "unknown"
	}
	return fmt.Sprintf("On-disk config (Ctrl+R refresh)\nDefault model: %s\nContext window: %s tokens\nMax output (maxTokens): %s tokens\nReasoning effort: %s\nUnknown = not recorded/read; running sessions may differ.",
		model, formatTokens(info.ContextWindow), formatTokens(info.MaxTokens), effort)
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
			return "Edit endpoint URL for " + m.editingModel.Slug + ":"
		}
		return "Model Library — endpoint URL:"
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
			return "Edit API base URL:"
		case fieldToken:
			return "Edit API key (hidden):"
		}
	}
	switch m.view {
	case viewAddEndpoint:
		if m.tool.DefaultEndpoint != "" {
			return "API base URL — leave blank for the default (" + m.tool.DefaultEndpoint + "):"
		}
		return "API base URL:"
	case viewAddKey:
		return "API key — input is hidden as you type:"
	case viewAddName:
		return "Name this binding (e.g. work, openrouter-fast):"
	case viewAddCustomModel:
		return "Enter model IDs — comma-separated (e.g. kimi-k3:1m, z-ai/glm-5.3, custom:200k):"
	case viewDupName:
		return "Name the duplicate of " + m.dupSource + ":"
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
	sub := promptStyle.Render("Review models to register:") + "\n\n"

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

	hdrRow := fmt.Sprintf("  %-3s %-*s  %-14s  %s", "#", maxSlugLen, "Model ID", "Context Window", "Source")
	divider := "  " + strings.Repeat("─", maxSlugLen+34)
	lines := []string{hintStyle.Render(hdrRow), hintStyle.Render(divider)}

	for i, slug := range m.wiz.models {
		win := m.wiz.windows[slug]
		winStr := formatTokens(win)
		src := windowSources[slug]

		sourceTag := "[unknown]"
		switch src {
		case catalog.WindowManual:
			sourceTag = "[manual]"
		case catalog.WindowBuiltin:
			if models.IsKnownBuiltin(slug) {
				sourceTag = "[builtin]"
			} else {
				sourceTag = "[fallback: 500K] ✎"
			}
		}

		cursorMark := "  "
		numStr := fmt.Sprintf("%d.", i+1)
		displaySlug := slug
		if len(displaySlug) > maxSlugLen {
			displaySlug = displaySlug[:maxSlugLen-1] + "…"
		}

		if i == m.reviewCursor {
			cursorMark = "❯ "
			row := fmt.Sprintf("%s%-3s %-*s  %-14s  %s", cursorMark, numStr, maxSlugLen, displaySlug, winStr, sourceTag)
			lines = append(lines, promptStyle.Render(row))
		} else {
			row := fmt.Sprintf("%s%-3s %-*s  %-14s  %s", cursorMark, numStr, maxSlugLen, displaySlug, winStr, hintStyle.Render(sourceTag))
			lines = append(lines, row)
		}
	}

	lines = append(lines, hintStyle.Render(divider))
	lines = append(lines, hintStyle.Render("Press w/e to edit context, d to remove row, enter to confirm."))

	body := header + sub + strings.Join(lines, "\n")
	return m.withFooter(body, keyConfirmReview, keyEditContext, keyDeleteRow, keyMove, keyEsc)
}
