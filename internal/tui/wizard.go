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

// Focus positions on the single-page add/edit form: four text inputs followed by the
// action rows. Named so the wrap-around arithmetic and the renderer can't drift apart.
const (
	focusName int = iota
	focusURL
	focusToken
	focusModel
	focusFetch  // [ Fetch & Pick Online Models ]
	focusManual // [ Type Model IDs Manually ]
	focusSave
	focusCancel
	focusCount // number of focusable positions
)

// formInputCount is how many of those positions are text inputs.
const formInputCount = focusFetch

type wizard struct {
	endpoint, key, model string
	name                 string // target profile name when editing
	origName             string // pre-edit name, to clean up on rename
	edit                 bool   // true = overwrite an existing profile
	// models is the curated list to register in the tool's own model picker, built by
	// space-toggling rows in the picker. Empty means "offer whatever was fetched", which
	// keeps the pick-one flow registering the full list as it always has.
	models []string
}

// modelIDs is the wizard's model choice as a flat list: the curated selection with the
// default model first, or just the default when nothing is curated. Used to prefill the
// manual-entry screen so an edit never asks the user to retype ids charon already knows.
func (w wizard) modelIDs() []string {
	ids := make([]string, 0, len(w.models)+1)
	if w.model != "" {
		ids = append(ids, w.model)
	}
	for _, id := range w.models {
		if id != w.model {
			ids = append(ids, id)
		}
	}
	return ids
}

// modelField is what the edit form's Model Slug field shows: the default model only.
// A curated picker list can run to dozens of ids, so it is summarized separately
// (see pickerNote) rather than dumped into a text field.
func (w wizard) modelField() string { return w.model }

// setModelField parses that field back. A single id just changes the default model and
// leaves any curated list intact; a comma-separated value curates the list outright,
// which is how you register models for a gateway that has no /v1/models to fetch.
func (w *wizard) setModelField(val string) {
	ids := splitModelIDs(val)
	switch len(ids) {
	case 0:
		w.model = ""
	case 1:
		w.model = ids[0]
	default:
		w.model, w.models = ids[0], ids
	}
}

// pickerNote summarizes the curated model list for the edit form, naming the tool's
// own model-switching menu (e.g. /model, /models) when it has one.
func (w wizard) pickerNote(t *tools.Tool) string {
	if len(w.models) == 0 {
		return ""
	}
	if t != nil && t.ModelMenu != "" {
		return fmt.Sprintf("%d model(s) offered in %s", len(w.models), t.ModelMenu)
	}
	return fmt.Sprintf("%d model(s) offered in the tool's own picker", len(w.models))
}

func (m model) pickerNote() string {
	return m.wiz.pickerNote(m.tool)
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
	case viewAddCustomModel:
		return 3, 4, "type the model ids"
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
	m.formFocus = focusName
	m.formInputs = make([]textinput.Model, formInputCount)
	m.formInputs[focusName] = newFormInput("e.g. openrouter-fast", m.wiz.name, false)
	m.formInputs[focusURL] = newFormInput(exampleEndpoint, m.wiz.endpoint, false)
	m.formInputs[focusToken] = newFormInput("sk-or-v1-xxxxxxxx", m.wiz.key, false)
	modelPlaceholder := "e.g. gpt-4o (leave blank for default)"
	if m.tool != nil && m.tool.Name == "claude" {
		modelPlaceholder = "e.g. claude-3-7-sonnet (leave blank for default)"
	}
	m.formInputs[focusModel] = newFormInput(modelPlaceholder, m.wiz.modelField(), false)
	m.formInputs[focusName].Focus()
}

func (m model) updateEditForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.dupSource = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles("")
		return m, nil
	case "up", "shift+tab":
		m.formFocus = (m.formFocus - 1 + focusCount) % focusCount
		return m.syncFormFocus()
	case "down", "tab":
		m.formFocus = (m.formFocus + 1) % focusCount
		return m.syncFormFocus()
	case "enter":
		if m.formFocus == focusFetch {
			endpoint := strings.TrimRight(strings.TrimSpace(m.formInputs[focusURL].Value()), "/")
			key := strings.TrimSpace(m.formInputs[focusToken].Value())
			if endpoint == "" && key == "" {
				m.setStatus(statusErr, "API URL & Key required")
				return m, nil
			}
			if endpoint == "" {
				m.setStatus(statusErr, "API URL required")
				return m, nil
			}
			if key == "" && !strings.Contains(endpoint, "localhost") && !strings.Contains(endpoint, "127.0.0.1") {
				m.setStatus(statusErr, "API Key required")
				return m, nil
			}
			m.wiz.endpoint = endpoint
			m.wiz.key = key
			m.fromForm = true
			cmd := m.beginFetch()
			return m, cmd
		}
		if m.formFocus == focusManual {
			// No fetch needed: endpoints without /v1/models still need their ids registered.
			m.fromForm = true
			m.clearStatus()
			return m.startManualModels()
		}
		if m.formFocus == focusSave {
			return m.submitForm()
		}
		if m.formFocus == focusCancel {
			m.dupSource = ""
			m.view = viewProfiles
			m.setStatus(statusInfo, "cancelled")
			m.loadProfiles("")
			return m, nil
		}
		m.formFocus = (m.formFocus + 1) % focusCount
		return m.syncFormFocus()
	}

	if m.formFocus < formInputCount {
		var cmd tea.Cmd
		m.formInputs[m.formFocus], cmd = m.formInputs[m.formFocus].Update(msg)
		m.wiz.name = strings.TrimSpace(m.formInputs[focusName].Value())
		m.wiz.endpoint = strings.TrimRight(strings.TrimSpace(m.formInputs[focusURL].Value()), "/")
		m.wiz.key = strings.TrimSpace(m.formInputs[focusToken].Value())
		m.wiz.setModelField(m.formInputs[focusModel].Value())
		return m, cmd
	}
	return m, nil
}

func (m *model) syncFormFocus() (tea.Model, tea.Cmd) {
	for i := 0; i < formInputCount; i++ {
		if i == m.formFocus {
			m.formInputs[i].Focus()
		} else {
			m.formInputs[i].Blur()
		}
	}
	return *m, textinput.Blink
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.formInputs[focusName].Value())
	if name == "" && m.wiz.origName != profile.DefaultName {
		m.setStatus(statusErr, "Profile Name is required")
		return m, nil
	}
	if name == "" {
		name = m.wiz.name
	}

	endpoint := strings.TrimRight(strings.TrimSpace(m.formInputs[focusURL].Value()), "/")
	if endpoint == "" {
		m.setStatus(statusErr, "API Base URL is required")
		return m, nil
	}

	key := strings.TrimSpace(m.formInputs[focusToken].Value())
	if key == "" {
		m.setStatus(statusErr, "API Key is required")
		return m, nil
	}

	m.wiz.endpoint = endpoint
	m.wiz.key = key
	m.wiz.setModelField(m.formInputs[focusModel].Value())
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
			// Only the picker can be gone back to when a fetch actually produced a list;
			// manual entry is also where a failed fetch lands, and returning to an empty
			// picker would strand the user on a blank screen.
			if len(m.allModels) > 0 {
				m.view = viewPickModel
				m.clearStatus()
				m.renderModels()
				return m, nil
			}
			if m.fromForm || m.wiz.edit {
				m.fromForm = false
				m.view = viewEditForm
				m.clearStatus()
				m.loadEditForm()
				return m, nil
			}
			m.view = viewAddKey
			m.clearStatus()
			m.startInput("API key", true)
			m.input.SetValue(m.wiz.key)
			return m, textinput.Blink
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
			// The typed ids are the list registered with the tool, so an endpoint with no
			// /v1/models still gets a full model menu; the first id is the default model.
			ids := splitModelIDs(val)
			m.wiz.models = ids
			if len(ids) == 0 {
				m.wiz.model = ""
			} else {
				m.wiz.model = ids[0]
			}
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
			if err := m.store.Duplicate(m.tool.Name, src, val); err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			m.dupSource = ""
			m.view = viewProfiles
			m.setStatus(statusOK, "Duplicated "+src+" → "+val)
			// Stay on the source row rather than jumping to the new duplicate.
			m.loadProfiles(src)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleConfirmDelete handles the enter/esc prompt on the confirmation dialog overlay.
func (m model) handleConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		name := m.delTarget
		m.delTarget = ""
		m.showConfirm = false
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
	case "esc":
		name := m.delTarget
		m.delTarget = ""
		m.showConfirm = false
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
	spec := profile.Spec{Endpoint: m.wiz.endpoint, Key: m.wiz.key, Model: m.wiz.model, Models: m.pickerModels()}
	verb := "Added"
	var err error
	if m.wiz.edit {
		verb = "Updated"
		err = m.store.EditProfile(m.tool, m.wiz.origName, name, spec)
	} else {
		err = m.store.AddProfile(m.tool, name, spec)
	}
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	stored, _ := m.store.GetSpec(m.tool.Name, name)
	m.setStatus(statusOK, fmt.Sprintf("%s %s (%s · %s)", verb, name, m.wiz.endpoint, describeSpecModels(stored)))
	m.view = viewProfiles
	m.loadProfiles(name) // land on the profile just added/edited, not wherever is active
	return m, nil
}

// splitModelIDs parses a typed model field ("a, b ,c") into ids, dropping blanks so a
// trailing comma or empty input yields no ids at all.
func splitModelIDs(val string) []string {
	var ids []string
	for _, id := range strings.Split(val, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// pickerModels is the model list to register with the tool: the curated selection when
// the user checked any rows, else the whole fetched list — preserving the long-standing
// behavior where picking one model still registers everything the endpoint offers.
func (m model) pickerModels() []string {
	if len(m.wiz.models) > 0 {
		return m.wiz.models
	}
	return m.allModels
}

// describeSpecModels summarizes a saved spec's model choice for the footer.
func describeSpecModels(spec profile.Spec) string {
	if spec.Model == "" {
		return "no model override"
	}
	if extra := len(spec.ModelIDs()) - 1; extra > 0 {
		return fmt.Sprintf("%s +%d more in the tool's picker", spec.Model, extra)
	}
	return spec.Model
}
