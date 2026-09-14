package tui

import (
	"fmt"
	"strings"

	"charon/internal/secret"

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
			" Add a profile per model to switch between them."
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

func (m model) View() string {
	switch m.view {
	case viewFetching:
		return m.wizardHeader() +
			promptStyle.Render(m.spinner.View()+m.loadingMsg) +
			"\n\n" + hintStyle.Render("fetching models from "+m.wiz.endpoint)
	case viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField, viewAddCustomModel:
		body := m.wizardHeader() +
			promptStyle.Render(m.prompt()) +
			"\n\n  " + m.input.View() +
			"\n\n" + hintStyle.Render(m.optionsHelp())
		if m.view == viewAddCustomModel {
			body += "\n\n" + hintStyle.Render(m.modelMenuNote())
		}
		if line := statusRender(m.statusLvl, m.status); line != "" {
			body += "\n" + line
		}
		return body
	case viewEditForm:
		title := m.tool.Title + " · Edit Profile"
		if !m.wiz.edit {
			title = m.tool.Title + " · New Profile"
		}
		header := "\n" + titleStyle.Render(title) + "\n\n"

		labels := []string{"Profile Name ", "API Base URL ", "API Key/Token", "Model Slug   "}
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

		saveBtn := "  [ Save Profile ]"
		cancelBtn := "  [ Cancel ]"
		if m.formFocus == focusSave {
			saveBtn = promptStyle.Render("▌ [ Save Profile ]")
		} else {
			saveBtn = hintStyle.Render("  [ Save Profile ]")
		}
		if m.formFocus == focusCancel {
			cancelBtn = promptStyle.Render("▌ [ Cancel ]")
		} else {
			cancelBtn = hintStyle.Render("  [ Cancel ]")
		}

		btnLine := "\n  " + saveBtn + "\n  " + cancelBtn
		hint := "\n\n" + hintStyle.Render("Shortcuts: ↑/↓: move · tab: switch · ctrl+s: save · esc: cancel")

		body := header + strings.Join(formLines, "\n") + "\n" + btnLine + hint
		if line := statusRender(m.statusLvl, m.status); line != "" {
			body += "\n" + line
		}
		return body
	}

	if m.showConfirm {
		return m.confirmDialog()
	}
	out := m.list.View()
	if m.view == viewPickModel {
		tip := `💡 Tip: type to search · enter sets the default model`
		if m.tool != nil {
			if m.tool.ModelMenu != "" {
				tip = `💡 Tip: space selects · ctrl+a selects all for ` + m.tool.ModelMenu +
					` · enter sets default & returns`
			} else {
				tip = `💡 Tip: enter chooses a model · type to search (` + m.tool.Title + ` only supports a single model)`
			}
		}
		if m.modelFilter != "" {
			tip = fmt.Sprintf(`🔍 Filter: %q (%d matches) · Esc: clear filter`, m.modelFilter, len(m.list.Items())-2)
		}
		out += "\n\n" + hintStyle.Render(tip)
	} else if m.view == viewTools {
		out = banner(m.version) + "\n\n" + out // blank line between the banner and the list title
	}
	if line := statusRender(m.statusLvl, m.status); line != "" {
		out += "\n" + line
	}
	return out
}

// wizardHeader renders the titled bar for add-flow screens.
func (m model) wizardHeader() string {
	n, total, label := wizardStep(m.view)
	if total == 0 {
		return "\n"
	}
	title := titleStyle.Render(m.tool.Title + " · new profile")
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
		return "Name this profile (e.g. work, openrouter-fast):"
	case viewAddCustomModel:
		return "Enter model IDs — comma-separated registers them all (first is the default):"
	case viewDupName:
		return "Name the duplicate of " + m.dupSource + ":"
	default:
		return ""
	}
}

func (m model) optionsHelp() string {
	switch m.view {
	case viewAddEndpoint:
		return "Options:\n  • [ Enter ] Continue to API Key\n  • [ Esc   ] Cancel & Return"
	case viewAddKey:
		return "Options:\n  • [ Enter ] Continue to Fetch Models\n  • [ Esc   ] ← Back to API Base URL"
	case viewAddCustomModel:
		return "Options:\n  • [ Enter ] Register These Model IDs\n  • [ Esc   ] ← Back"
	case viewAddName:
		if len(m.allModels) > 0 {
			return "Options:\n  • [ Enter ] Save Profile\n  • [ Esc   ] ← Back to Model Selection"
		}
		return "Options:\n  • [ Enter ] Save Profile\n  • [ Esc   ] ← Back to API Key"
	case viewEditField:
		return "Options:\n  • [ Enter ] Save Field\n  • [ Esc   ] Cancel Field Edit"
	case viewDupName:
		return "Options:\n  • [ Enter ] Duplicate Profile\n  • [ Esc   ] Cancel"
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

	content := warnStyle.Render("Delete profile "+m.delTarget+"?") + "\n" +
		"This can't be undone.\n\n" +
		hintStyle.Render("enter: delete · esc: cancel")

	dialog := dialogStyle.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}
