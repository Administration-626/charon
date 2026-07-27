package tui

import (
	"fmt"
	"strings"

	"charon/internal/profile"
	"charon/internal/secret"
	"charon/internal/tools"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	fieldName    = "\x00name"
	fieldURL     = "\x00url"
	fieldToken   = "\x00token"
	fieldModel   = "\x00model"
	actionSave   = "\x00save"
	actionCancel = "\x00cancel"
)

type wizard struct {
	endpoint, key, model string
	name                 string // target profile name when editing
	origName             string // pre-edit name, to clean up on rename
	edit                 bool   // true = overwrite an existing profile
}

// wizardStep maps an add-flow view to its step index, total, and label (total 0 = no progress).
func wizardStep(v view) (n, total int, label string) {
	switch v {
	case viewAddEndpoint:
		return 1, 4, "API base URL"
	case viewAddKey:
		return 2, 4, "API key"
	case viewFetching, viewPickModel:
		return 3, 4, "choose a model"
	case viewAddName:
		return 4, 4, "name the profile"
	}
	return 0, 0, ""
}

// loadEditForm populates the field picker from the working wizard values.
func (m *model) loadEditForm() {
	token := "(none — required)"
	if m.wiz.key != "" {
		token = secret.Mask(m.wiz.key)
	}
	modelVal := m.wiz.model
	if modelVal == "" {
		modelVal = "(none — use default)"
	}
	endpoint := m.wiz.endpoint
	if endpoint == "" {
		endpoint = "(none — use default)"
	}
	nameVal := m.wiz.name
	if nameVal == "" {
		nameVal = "(none — required)"
	}
	items := []list.Item{}
	if m.wiz.origName != profile.DefaultName {
		items = append(items, item{title: "Profile Name", desc: nameVal, value: fieldName})
	}
	items = append(items,
		item{title: "API Base URL", desc: endpoint, value: fieldURL},
		item{title: "API Key/Token", desc: token, value: fieldToken},
		item{title: "Model Slug", desc: modelVal + "  (press m to fetch & pick)", value: fieldModel},
		item{value: sepSentinel},
		item{title: "[ Save Profile ]", desc: "Submit and save profile (Ctrl+S)", value: actionSave},
		item{title: "[ Cancel ]", desc: "Discard changes and exit (Esc)", value: actionCancel},
	)
	m.list.SetDelegate(themedDelegate()) // two-line rows show each field's value
	m.list.SetItems(items)
	if m.wiz.edit {
		m.list.Title = fmt.Sprintf("Edit %s / %s", m.tool.Title, m.wiz.name)
	} else {
		m.list.Title = fmt.Sprintf("Add %s Profile", m.tool.Title)
	}
	// Land on the field last visited; a fresh form falls back to the first row.
	m.list.Select(0)
	m.selectByValue(m.editField)
	m.setHelpKeys(
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move field")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit/select")),
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	)
}

// onEditFormSelect handles a chosen row in the edit field-picker.
func (m model) onEditFormSelect(field string) (tea.Model, tea.Cmd) {
	switch field {
	case actionSave:
		name := strings.TrimSpace(m.wiz.name)
		if name == "" {
			m.setStatus(statusErr, "profile name is required")
			return m, nil
		}
		return m.finishAdd(name)
	case actionCancel:
		m.dupSource = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles("")
		return m, nil
	case fieldName:
		if m.wiz.origName == profile.DefaultName {
			m.setStatus(statusInfo, "the default profile can't be renamed")
			return m, nil
		}
		m.editField = field
		m.startInput("profile name", false)
		m.input.SetValue(m.wiz.name)
		return m, textinput.Blink
	case fieldURL:
		m.editField = field
		m.startInput(exampleEndpoint, false)
		m.input.SetValue(m.wiz.endpoint)
		return m, textinput.Blink
	case fieldToken:
		m.editField = field
		m.startInput("API key", true)
		m.input.SetValue(m.wiz.key)
		return m, textinput.Blink
	case fieldModel:
		m.editField = fieldModel
		m.fromForm = true
		cmd := m.beginFetch()
		return m, cmd
	}
	return m, nil
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.view == viewEditForm {
		val := m.input.Value()
		switch m.editField {
		case fieldName:
			m.wiz.name = strings.TrimSpace(val)
		case fieldURL:
			m.wiz.endpoint = strings.TrimSpace(val)
		case fieldToken:
			m.wiz.key = strings.TrimSpace(val)
		case fieldModel:
			m.wiz.model = strings.TrimSpace(val)
		}

		switch msg.String() {
		case "ctrl+s":
			return m.onEditFormSelect(actionSave)
		case "esc":
			return m.onEsc()
		case "m", "ctrl+m":
			if m.editField == fieldModel {
				m.fromForm = true
				cmd := m.beginFetch()
				return m, cmd
			}
		}
	}

	switch msg.String() {
	case "esc":
		if m.view == viewEditForm && m.editField != "" {
			m.editField = ""
			m.loadEditForm()
			return m, nil
		}
		if m.view == viewEditField {
			m.view = viewEditForm // cancel a single field → back to the form
			m.loadEditForm()
			return m, nil
		}
		if m.view == viewAddCustomModel {
			m.view = viewPickModel
			m.clearStatus()
			return m, nil
		}
		if m.view == viewAddKey {
			m.view = viewAddEndpoint
			m.clearStatus()
			m.startInput(exampleEndpoint, false)
			m.input.SetValue(m.wiz.endpoint)
			return m, textinput.Blink
		}
		if m.view == viewAddName {
			if len(m.allModels) > 0 {
				m.view = viewPickModel
				m.clearStatus()
				return m, nil
			}
			m.view = viewAddKey
			m.clearStatus()
			m.startInput("API key", true)
			m.input.SetValue(m.wiz.key)
			return m, textinput.Blink
		}
		src := m.dupSource
		m.dupSource = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles(src) // land back on the profile that was being duplicated, if any
		return m, nil
	case "enter":
		val := m.input.Value()
		switch m.view {
		case viewEditField, viewEditForm:
			switch m.editField {
			case fieldName:
				val = strings.TrimSpace(val)
				if val == "" {
					m.setStatus(statusErr, "name is required")
					return m, nil
				}
				m.wiz.name = val
				m.editField = fieldURL
				m.startInput(exampleEndpoint, false)
				m.input.SetValue(m.wiz.endpoint)
				m.loadEditForm()
				return m, textinput.Blink
			case fieldURL:
				val = strings.TrimSpace(val)
				if err := tools.ValidateEndpoint(val); err != nil {
					m.setStatus(statusErr, err.Error())
					return m, nil
				}
				m.wiz.endpoint = val
				m.editField = fieldToken
				m.startInput("API key", true)
				m.input.SetValue(m.wiz.key)
				m.loadEditForm()
				return m, textinput.Blink
			case fieldToken:
				val = strings.TrimSpace(val)
				if err := tools.ValidateKey(val); err != nil {
					m.setStatus(statusErr, err.Error())
					return m, nil
				}
				m.wiz.key = val
				m.editField = ""
				m.clearStatus()
				m.loadEditForm()
				return m, nil
			}

		case viewAddEndpoint:
			val = strings.TrimSpace(val)
			if err := tools.ValidateEndpoint(val); err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			m.wiz.endpoint = m.tool.ResolveEndpoint(val) // blank accepts the provider default
			m.view = viewAddKey
			m.clearStatus()
			m.startInput("API key", true)
			return m, textinput.Blink

		case viewAddKey:
			val = strings.TrimSpace(val)
			if err := tools.ValidateKey(val); err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			m.wiz.key = val
			m.clearStatus()
			cmd := m.beginFetch()
			return m, cmd

		case viewAddCustomModel:
			val = strings.TrimSpace(val)
			m.wiz.model = val
			m.clearStatus()
			if m.fromForm {
				m.fromForm = false
				m.editField = ""
				return m.finishAdd(m.wiz.name)
			}
			if m.wiz.edit {
				return m.finishAdd(m.wiz.name)
			}
			m.view = viewAddName
			m.startInput("profile name (e.g. openrouter-fast)", false)
			return m, textinput.Blink

		case viewAddName:
			val = strings.TrimSpace(val)
			if val == "" {
				m.setStatus(statusErr, "name is required")
				return m, nil
			}
			return m.finishAdd(val)

		case viewDupName:
			val = strings.TrimSpace(val)
			if val == "" {
				m.setStatus(statusErr, "name is required")
				return m, nil
			}
			src := m.dupSource
			m.dupSource = ""
			m.view = viewProfiles
			if err := m.store.Duplicate(m.tool.Name, src, val); err != nil {
				m.setStatus(statusErr, err.Error())
			} else {
				m.setStatus(statusOK, "Duplicated "+src+" → "+val)
			}
			// Stay on the source row rather than jumping to the new duplicate.
			m.loadProfiles(src)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateConfirmDelete handles the y/n prompt guarding profile deletion.
func (m model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		name := m.delTarget
		m.delTarget = ""
		m.view = viewProfiles
		if m.store.Active(m.tool.Name) == name {
			if _, err := m.store.Apply(m.tool, profile.DefaultName); err != nil {
				m.setStatus(statusErr, err.Error())
				m.loadProfiles(name)
				return m, nil
			}
		}
		if err := m.store.Remove(m.tool.Name, name); err != nil {
			m.setStatus(statusErr, err.Error())
			m.loadProfiles(name)
		} else {
			m.setStatus(statusOK, "Deleted "+name)
			m.loadProfiles("") // the row is gone; fall back to the active profile
		}
		return m, nil
	case "n", "N", "esc":
		name := m.delTarget
		m.delTarget = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles(name)
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// finishAdd applies the wizard's endpoint/key/model and snapshots it as the named
// profile — via EditProfile when editing, so a rename also cleans up the old name.
func (m model) finishAdd(name string) (tea.Model, tea.Cmd) {
	spec := profile.Spec{Endpoint: m.wiz.endpoint, Key: m.wiz.key, Model: m.wiz.model}
	verb := "Added"
	var err error
	if m.wiz.edit {
		verb = "Updated"
		err = m.store.EditProfile(m.tool, m.wiz.origName, name, spec, m.allModels...)
	} else {
		err = m.store.AddProfile(m.tool, name, spec, m.allModels...)
	}
	if err != nil {
		m.setStatus(statusErr, err.Error())
	} else {
		model := m.wiz.model
		if model == "" {
			model = "no model override"
		}
		m.setStatus(statusOK, fmt.Sprintf("%s %s (%s · %s)", verb, name, m.wiz.endpoint, model))
	}
	m.view = viewProfiles
	m.loadProfiles(name) // land on the profile just added/edited, not wherever is active
	return m, nil
}
