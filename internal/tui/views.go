package tui

import (
	"fmt"
	"strings"

	"charon/internal/secret"

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
	}
	return []key.Binding{keyContinue, keyEsc}
}

func (m model) View() string {
	switch m.view {
	case viewFetching:
		return m.withFooter(m.wizardHeader() +
			promptStyle.Render(m.spinner.View()+m.loadingMsg) +
			"\n\n" + hintStyle.Render("fetching models from "+m.wiz.endpoint))
	case viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField, viewAddCustomModel:
		body := m.wizardHeader() +
			promptStyle.Render(m.prompt()) +
			"\n\n  " + m.input.View()
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

		labels := []string{"Name         ", "API Base URL ", "API Key/Token", "Model Slug   "}
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
			if i == focusModel {
				// Tree-indented model actions under Model Slug: fetch the endpoint's list,
				// or type ids by hand for an endpoint that serves no /v1/models.
				formLines = append(formLines,
					modelActionRow("├── [ Fetch & Pick Online Models ]", m.formFocus == focusFetch),
					modelActionRow("└── [ Type Model IDs Manually ]", m.formFocus == focusManual))
				if note := m.pickerNote(); note != "" {
					formLines = append(formLines, hintStyle.Render("                       "+note))
				}
			}
		}

		saveBtn := hintStyle.Render("  [ Save ]")
		if m.formFocus == focusSave {
			saveBtn = promptStyle.Render("▌ [ Save ]")
		}
		cancelBtn := hintStyle.Render("  [ Cancel ]")
		if m.formFocus == focusCancel {
			cancelBtn = promptStyle.Render("▌ [ Cancel ]")
		}

		body := header + strings.Join(formLines, "\n") + "\n  " + saveBtn + "\n  " + cancelBtn
		return m.withFooter(body, keyMove, keyNextField, keySelectAction, keyCancel)
	}

	if m.showConfirm {
		return m.confirmDialog()
	}
	out := m.list.View()
	if m.view == viewTools {
		out = banner(m.version) + "\n\n" + out // blank line between the banner and the list title
	}
	return m.withFooter(out, m.footerKeys...)
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
	if m.view == viewEditField {
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
		return "Enter model IDs — comma-separated registers them all (first is the default):"
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
