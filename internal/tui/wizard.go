package tui

import (
	"fmt"
	"strings"

	"charon/internal/profile"
	"charon/internal/tools"

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

func newFormInput(placeholder, value string, isPassword bool) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 256
	ti.Width = 40
	if isPassword {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	if value != "" {
		ti.SetValue(value)
	}
	return ti
}

// loadEditForm populates the native multi-input form.
func (m *model) loadEditForm() {
	m.formFocus = 0
	m.formInputs = make([]textinput.Model, 4)
	m.formInputs[0] = newFormInput("e.g. openrouter-fast", m.wiz.name, false)
	m.formInputs[1] = newFormInput(exampleEndpoint, m.wiz.endpoint, false)
	m.formInputs[2] = newFormInput("sk-or-v1-xxxxxxxx", m.wiz.key, true)
	m.formInputs[3] = newFormInput("gpt-4o (press m to pick)", m.wiz.model, false)
	m.formInputs[0].Focus()
}

func (m model) updateEditForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.dupSource = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles("")
		return m, nil
	case "ctrl+s":
		return m.submitForm()
	case "up", "shift+tab":
		m.formFocus = (m.formFocus - 1 + 6) % 6
		return m.syncFormFocus()
	case "down", "tab":
		m.formFocus = (m.formFocus + 1) % 6
		return m.syncFormFocus()
	case "enter":
		if m.formFocus == 4 { // [ Save Profile ]
			return m.submitForm()
		}
		if m.formFocus == 5 { // [ Cancel ]
			m.dupSource = ""
			m.view = viewProfiles
			m.setStatus(statusInfo, "cancelled")
			m.loadProfiles("")
			return m, nil
		}
		m.formFocus = (m.formFocus + 1) % 6
		return m.syncFormFocus()
	case "m", "ctrl+m":
		if m.formFocus == 3 {
			m.wiz.endpoint = strings.TrimSpace(m.formInputs[1].Value())
			m.wiz.key = strings.TrimSpace(m.formInputs[2].Value())
			m.fromForm = true
			cmd := m.beginFetch()
			return m, cmd
		}
	}

	if m.formFocus < 4 {
		var cmd tea.Cmd
		m.formInputs[m.formFocus], cmd = m.formInputs[m.formFocus].Update(msg)
		m.wiz.name = strings.TrimSpace(m.formInputs[0].Value())
		m.wiz.endpoint = strings.TrimSpace(m.formInputs[1].Value())
		m.wiz.key = strings.TrimSpace(m.formInputs[2].Value())
		m.wiz.model = strings.TrimSpace(m.formInputs[3].Value())
		return m, cmd
	}
	return m, nil
}

func (m *model) syncFormFocus() (tea.Model, tea.Cmd) {
	for i := 0; i < 4; i++ {
		if i == m.formFocus {
			m.formInputs[i].Focus()
		} else {
			m.formInputs[i].Blur()
		}
	}
	return *m, textinput.Blink
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.formInputs[0].Value())
	if name == "" && m.wiz.origName != profile.DefaultName {
		m.setStatus(statusErr, "profile name is required")
		return m, nil
	}
	if name == "" {
		name = m.wiz.name
	}
	m.wiz.endpoint = strings.TrimSpace(m.formInputs[1].Value())
	m.wiz.key = strings.TrimSpace(m.formInputs[2].Value())
	m.wiz.model = strings.TrimSpace(m.formInputs[3].Value())
	return m.finishAdd(name)
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
