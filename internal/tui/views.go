package tui

import (
	"fmt"
	"strings"
)

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
	case viewConfirmDelete:
		body := "\n" + titleStyle.Render(m.tool.Title+" · delete profile") +
			"\n\n" + warnStyle.Render(fmt.Sprintf("Delete profile %q? This can't be undone.", m.delTarget)) +
			"\n\n" + hintStyle.Render("y: delete · n / esc: cancel")
		return body
	case viewFetching:
		return m.wizardHeader() +
			promptStyle.Render(m.spinner.View()+m.loadingMsg) +
			"\n\n" + hintStyle.Render("fetching models from "+m.wiz.endpoint)
	case viewAddEndpoint, viewAddKey, viewAddName, viewDupName, viewEditField, viewAddCustomModel:
		body := m.wizardHeader() +
			promptStyle.Render(m.prompt()) +
			"\n\n  " + m.input.View() +
			"\n\n" + hintStyle.Render(m.optionsHelp())
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

		for i := 0; i < 4; i++ {
			if m.formFocus == i {
				// Focused Row: Bold accent bar, bold label, clear input
				bar := promptStyle.Render("▌ ")
				labelStr := promptStyle.Render(labels[i] + " : ")
				inputStr := m.formInputs[i].View()
				if i == 3 {
					inputStr += hintStyle.Render(" (press m to pick)")
				}
				formLines = append(formLines, bar+labelStr+inputStr)
			} else {
				// Unfocused Row: Muted, low contrast
				bar := "  "
				labelStr := hintStyle.Render(labels[i] + " : ")
				inputVal := m.formInputs[i].Value()
				if inputVal == "" {
					inputVal = m.formInputs[i].Placeholder
				} else if i == 2 { // Password masking for Token
					inputVal = strings.Repeat("•", len(inputVal))
				}
				inputStr := hintStyle.Render(inputVal)
				formLines = append(formLines, bar+labelStr+inputStr)
			}
		}

		saveBtn := "[ Save Profile ]"
		cancelBtn := "[ Cancel ]"
		if m.formFocus == 4 {
			saveBtn = promptStyle.Render("▸ [ Save Profile ]")
		} else {
			saveBtn = hintStyle.Render("  [ Save Profile ]")
		}
		if m.formFocus == 5 {
			cancelBtn = promptStyle.Render("▸ [ Cancel ]")
		} else {
			cancelBtn = hintStyle.Render("  [ Cancel ]")
		}

		btnLine := "\n  " + saveBtn + "    " + cancelBtn
		hint := "\n\n" + hintStyle.Render("↑/↓ / tab: switch field · type/backspace: edit directly · ctrl+s: save · esc: cancel")

		body := header + strings.Join(formLines, "\n") + "\n" + btnLine + hint
		if line := statusRender(m.statusLvl, m.status); line != "" {
			body += "\n" + line
		}
		return body
	}

	out := m.list.View()
	if m.view == viewTools {
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
		return "Enter custom model ID (e.g. gpt-4o, claude-3-7-sonnet):"
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
		return "Options:\n  • [ Enter ] Use Custom Model ID\n  • [ Esc   ] ← Back to Model List"
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
