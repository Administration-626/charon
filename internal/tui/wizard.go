package tui

import (
	"fmt"
	"strings"

	"charon/internal/catalog"
	"charon/internal/models"
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

// Focus positions on the single-page add/edit form. Non-Pi tools skip the API selector.
const (
	focusName int = iota
	focusURL
	focusToken
	focusPiAPI
	focusModels // [ Models… ] — opens the list; f fetches inside it
	focusSave
	focusCancel
	focusCount // number of focusable positions
)

// formInputCount is how many of those positions are text inputs.
const formInputCount = focusPiAPI

var piAPIOptions = []struct {
	label string
	api   string
}{
	{"OpenAI (Chat Completions)", "openai-completions"},
	{"OpenAI (Responses)", "openai-responses"},
	{"Anthropic (Messages)", "anthropic-messages"},
	{"Gemini (Generate Content)", "google-generative-ai"},
}

func piAPILabel(api string) string {
	if api == "" {
		api, _ = tools.ResolvePiAPI("")
	}
	for _, option := range piAPIOptions {
		if option.api == api {
			return option.label
		}
	}
	return api
}

type wizard struct {
	endpoint, key, model string
	piAPI                string
	name                 string // target binding name when editing
	origName             string // pre-edit name, to clean up on rename
	edit                 bool   // true = overwrite an existing binding
	// models is the curated list to register in the tool's own model picker, built by
	// space-toggling rows in the picker. Empty means "offer whatever was fetched", which
	// keeps the pick-one flow registering the full list as it always has.
	models        []string
	windows       map[string]int
	manualWindows map[string]bool
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
		return 1, 4, "endpoint"
	case viewAddKey:
		return 2, 4, "API key"
	case viewFetching, viewPickModel:
		return 3, 4, "choose a model"
	case viewReviewModels:
		return 3, 4, "models & context"
	case viewAddName:
		return 4, 4, "name the binding"
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

// loadEditForm populates the native multi-input form. The first load after the form
// opens captures the baseline the dirty guard compares against; returning from a
// sub-screen reloads the values without moving that baseline.
func (m *model) loadEditForm() {
	if !m.formBaselineSet {
		m.formBaseline = captureForm(m.wiz)
		m.formBaselineSet = true
	}
	m.formFocus = focusName
	m.piAPIOpen = false
	if m.tool.Name == "pi" && m.wiz.piAPI == "" {
		m.wiz.piAPI, _ = tools.ResolvePiAPI("")
	}
	m.formInputs = make([]textinput.Model, formInputCount)
	m.formInputs[focusName] = newFormInput("e.g. openrouter-fast", m.wiz.name, false)
	m.formInputs[focusURL] = newFormInput(exampleEndpoint, m.wiz.endpoint, false)
	m.formInputs[focusToken] = newFormInput("sk-or-v1-xxxxxxxx", m.wiz.key, false)
	m.formInputs[focusName].Focus()
}

// loadEditFormAt repopulates the form with the cursor left on focus, so stepping back
// from a sub-screen (the picker, manual model ids) keeps the highlighted row where it
// was instead of jumping to the name row. A fresh open uses loadEditForm instead.
func (m *model) loadEditFormAt(focus int) {
	m.loadEditForm()
	m.formFocus = focus
	m.applyFormFocus()
}

func (m model) updateEditForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.piAPIOpen {
		return m.updatePiAPI(msg)
	}
	key := msg.String()
	if key != "esc" {
		// Any other key disarms the discard prompt: esc has to be pressed twice
		// in a row, so a stray esc before continuing to edit cannot lose work.
		m.discardArmed = false
	}
	switch key {
	case "esc":
		if m.discardArmed || !m.formDirty() {
			m.leaveEditForm("cancelled")
			return m, nil
		}
		m.discardArmed = true
		if m.formFocus < formInputCount {
			// Leave the field without losing what was typed; the next esc
			// discards the whole form.
			m.formFocus = focusModels
			m.applyFormFocus()
			m.setStatus(statusWarn, "edits kept — s saves, esc again discards")
		} else {
			m.setStatus(statusWarn, "unsaved changes — s saves, esc again discards")
		}
		return m, nil
	case "up", "shift+tab":
		m.moveFormFocus(-1)
		return m.syncFormFocus()
	case "down", "tab":
		m.moveFormFocus(1)
		return m.syncFormFocus()
	case "s":
		if m.formFocus >= formInputCount {
			return m.submitForm()
		}
	case "enter":
		if m.formFocus == focusPiAPI && m.tool.Name == "pi" {
			m.piAPIOpen = true
			m.piAPICursor = 0 // default: OpenAI Chat Completions
			for i, option := range piAPIOptions {
				if option.api == m.wiz.piAPI {
					m.piAPICursor = i
					break
				}
			}
			m.clearStatus()
			return m, nil
		}
		if m.formFocus == focusModels {
			// One entry point for models: the list opens immediately and f
			// fetches from the endpoint inside it.
			m.fromForm = true
			m.clearStatus()
			return m.startManualModels()
		}
		if m.formFocus == focusSave {
			return m.submitForm()
		}
		if m.formFocus == focusCancel {
			m.leaveEditForm("cancelled")
			return m, nil
		}
		m.moveFormFocus(1)
		return m.syncFormFocus()
	}

	if m.formFocus < formInputCount {
		var cmd tea.Cmd
		m.formInputs[m.formFocus], cmd = m.formInputs[m.formFocus].Update(msg)
		m.wiz.name = strings.TrimSpace(m.formInputs[focusName].Value())
		m.wiz.endpoint = strings.TrimRight(strings.TrimSpace(m.formInputs[focusURL].Value()), "/")
		m.wiz.key = strings.TrimSpace(m.formInputs[focusToken].Value())
		return m, cmd
	}
	return m, nil
}

// leaveEditForm closes the form and returns to the binding list, clearing the
// dirty-guard state so the next open starts clean.
func (m *model) leaveEditForm(status string) {
	m.discardArmed = false
	m.formBaselineSet = false
	m.editField = ""
	m.dupSource = ""
	m.dupTool = ""
	m.view = viewProfiles
	m.setStatus(statusInfo, status)
	m.loadProfiles("")
}

// formSnapshot is the form's own state when it was opened. Model maps are copied
// because the wizard keeps mutating them while the picker and list are open.
type formSnapshot struct {
	name, endpoint, key, piAPI, model string
	models                            []string
	windows                           map[string]int
	manualWindows                     map[string]bool
}

func captureForm(w wizard) formSnapshot {
	s := formSnapshot{
		name:     w.name,
		endpoint: w.endpoint,
		key:      w.key,
		piAPI:    w.piAPI,
		model:    w.model,
		models:   append([]string(nil), w.models...),
	}
	if len(w.windows) > 0 {
		s.windows = make(map[string]int, len(w.windows))
		for k, v := range w.windows {
			s.windows[k] = v
		}
	}
	if len(w.manualWindows) > 0 {
		s.manualWindows = make(map[string]bool, len(w.manualWindows))
		for k, v := range w.manualWindows {
			s.manualWindows[k] = v
		}
	}
	return s
}

// formDirty reports whether the form would write something different from what
// it opened with. Only windows of currently selected models count: a fetch seeds
// windows for every offered model, and those alone must not look like an edit.
func (m model) formDirty() bool {
	if !m.formBaselineSet {
		return false
	}
	s := m.formBaseline
	if m.wiz.name != s.name || m.wiz.endpoint != s.endpoint || m.wiz.key != s.key ||
		m.wiz.piAPI != s.piAPI || m.wiz.model != s.model {
		return true
	}
	if len(m.wiz.models) != len(s.models) {
		return true
	}
	for i, id := range m.wiz.models {
		if id != s.models[i] {
			return true
		}
		if m.wiz.windows[id] != s.windows[id] || m.wiz.manualWindows[id] != s.manualWindows[id] {
			return true
		}
	}
	return false
}

func (m *model) moveFormFocus(delta int) {
	m.formFocus = (m.formFocus + delta + focusCount) % focusCount
	if m.formFocus == focusPiAPI && m.tool.Name != "pi" {
		m.formFocus += delta
	}
}

func (m model) updatePiAPI(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "shift+tab":
		m.piAPICursor = (m.piAPICursor - 1 + len(piAPIOptions)) % len(piAPIOptions)
	case "down", "tab":
		m.piAPICursor = (m.piAPICursor + 1) % len(piAPIOptions)
	case "esc":
		m.piAPIOpen = false
		m.clearStatus()
	case "enter":
		m.wiz.piAPI = piAPIOptions[m.piAPICursor].api
		m.piAPIOpen = false
		m.setStatus(statusInfo, "Selected "+piAPILabel(m.wiz.piAPI)+"; choose Save to store the binding")
	}
	return m, nil
}

func (m *model) syncFormFocus() (tea.Model, tea.Cmd) {
	m.applyFormFocus()
	return *m, textinput.Blink
}

// applyFormFocus focuses the input under formFocus and blurs the rest; an action row
// leaves every input blurred, so typing never lands in a field the cursor left.
func (m *model) applyFormFocus() {
	for i := 0; i < formInputCount; i++ {
		if i == m.formFocus {
			m.formInputs[i].Focus()
		} else {
			m.formInputs[i].Blur()
		}
	}
}

func (m model) submitForm() (tea.Model, tea.Cmd) {
	m.discardArmed = false
	name := strings.TrimSpace(m.formInputs[focusName].Value())
	if name == "" {
		m.setStatus(statusErr, "Name is required")
		return m, nil
	}

	endpoint := strings.TrimRight(strings.TrimSpace(m.formInputs[focusURL].Value()), "/")
	if endpoint == "" {
		m.setStatus(statusErr, "Endpoint is required")
		return m, nil
	}

	key := strings.TrimSpace(m.formInputs[focusToken].Value())
	if key == "" {
		m.setStatus(statusErr, "API Key is required")
		return m, nil
	}

	if m.wiz.model == "" && len(m.wiz.models) == 0 {
		m.setStatus(statusErr, "Model is required (choose below)")
		return m, nil
	}

	m.wiz.endpoint = endpoint
	m.wiz.key = key
	return m.finishAdd(name)
}

// onEditFormSelect handles a chosen row in the edit field-picker.
func (m model) onEditFormSelect(field string) (tea.Model, tea.Cmd) {
	switch field {
	case actionSave:
		name := strings.TrimSpace(m.wiz.name)
		if name == "" {
			m.setStatus(statusErr, "name is required")
			return m, nil
		}
		return m.finishAdd(name)
	case actionCancel:
		m.dupSource = ""
		m.dupTool = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles("")
		return m, nil
	case fieldName:
		m.editField = field
		m.startInput("binding name", false)
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

		// Deliberately no ctrl+s here: the terminal eats it as XOFF (flow control) and
		// the TUI freezes. Save is the [ Save ] row, reached with ↓/tab then enter.
		switch msg.String() {
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
		if m.view == viewModelEndpoint || m.view == viewModelSlug {
			m.view = viewModels
			m.clearStatus()
			m.loadModels("")
			return m, nil
		}
		if m.view == viewModelWindow {
			targetID := m.editingModel.ID
			m.editingModel = catalog.Model{}
			m.modelEditEndpoint = ""
			m.view = viewModels
			m.clearStatus()
			m.loadModels(targetID)
			return m, nil
		}
		if m.view == viewModelEffort {
			targetID := m.editingModel.ID
			m.editingModel = catalog.Model{}
			m.modelEditEndpoint = ""
			m.view = viewModels
			m.clearStatus()
			m.loadModels(targetID)
			return m, nil
		}
		if m.view == viewEditForm && m.editField != "" {
			m.editField = ""
			m.loadEditForm()
			return m, nil
		}
		if m.view == viewEditField {
			m.view = viewPickModel // cancel window editing → back to the picker
			m.editTarget = ""
			m.renderModels()
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
			if len(m.wiz.models) > 0 {
				m.view = viewReviewModels
				m.clearStatus()
				return m, nil
			}
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
		m.dupTool = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles(src) // land back on the binding that was being duplicated, if any
		return m, nil
	case "enter":
		val := m.input.Value()
		switch m.view {
		case viewEditField, viewEditForm:
			if m.editTarget != "" {
				val = strings.TrimSpace(val)
				window := 0
				if val != "" {
					var err error
					if window, err = parseContextWindow(val); err != nil {
						m.setStatus(statusErr, err.Error())
						return m, nil
					}
				}
				return m.applyWizardWindow(m.editTarget, window), nil
			}
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

		case viewAddName:
			val = strings.TrimSpace(val)
			if val == "" {
				m.setStatus(statusErr, "name is required")
				return m, nil
			}
			return m.finishAdd(val)

		case viewModelEndpoint:
			endpoint := strings.TrimRight(strings.TrimSpace(val), "/")
			if err := tools.ValidateEndpoint(endpoint); err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			m.modelEditEndpoint = endpoint
			m.view = viewModelSlug
			m.clearStatus()
			m.startInput("model slug, e.g. glm-4.7", false)
			if m.editingModel.ID != "" {
				m.input.SetValue(m.editingModel.Slug)
			}
			return m, textinput.Blink

		case viewModelSlug:
			slug := strings.TrimSpace(val)
			if slug == "" {
				m.setStatus(statusErr, "model slug is required")
				return m, nil
			}
			m.editingModel.Slug = slug
			m.openWindowPresets(false)
			return m, nil

		case viewModelWindow:
			window := 0
			if strings.TrimSpace(val) != "" {
				var err error
				if window, err = parseContextWindow(val); err != nil {
					m.setStatus(statusErr, err.Error())
					return m, nil
				}
			}
			return m.saveCatalogWindow(window)

		case viewModelEffort:
			levels, effort, err := parseThinkingLevelMap(val)
			if err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			saved, err := m.cat.SetModelEffort(m.editingModel.ID, effort)
			if err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			if _, err := m.cat.SetModelThinkingLevelMap(saved.ID, levels); err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			saved.ThinkingLevelMap = levels
			bindings, err := m.cat.Bindings("")
			if err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			for _, binding := range bindings {
				for _, modelID := range binding.Models {
					if modelID != saved.ID {
						continue
					}
					if _, err := m.cat.ProjectIfActive(binding.ID); err != nil {
						m.setStatus(statusErr, err.Error())
						m.view = viewModels
						m.loadModels(saved.ID)
						return m, nil
					}
				}
			}
			m.editingModel = catalog.Model{}
			m.modelEditEndpoint = ""
			m.view = viewModels
			m.loadModels(saved.ID)
			m.setStatus(statusOK, "Saved effort for "+saved.Slug)
			return m, nil

		case viewDupName:
			val = strings.TrimSpace(val)
			if val == "" {
				m.setStatus(statusErr, "name is required")
				return m, nil
			}
			src := m.dupSource
			dst := m.dupTool
			if err := m.duplicateBinding(src, dst, val); err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			m.dupSource = ""
			m.dupTool = ""
			m.view = viewProfiles
			if dst == m.tool.Name {
				m.setStatus(statusOK, "Duplicated "+src+" → "+val)
				m.loadProfiles(val) // land the cursor on the new clone
				return m, nil
			}
			if t := m.findTool(dst); t != nil {
				m.setStatus(statusOK, "Duplicated "+src+" → "+val+" in "+t.Title)
			}
			m.loadProfiles(src) // stay on the source binding in this tool
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
		if active, found, err := m.cat.Active(m.tool.Name); err == nil && found && active.Name == name {
			m.setStatus(statusErr, "switch to another binding before deleting the active one")
			m.loadProfiles(name)
			return m, nil
		}
		b, found, err := m.cat.BindingByName(m.tool.Name, name)
		if err != nil || !found {
			m.setStatus(statusErr, "no binding named "+name)
			m.loadProfiles("")
			return m, nil
		}
		if err := m.cat.RemoveBinding(b.ID); err != nil {
			m.setStatus(statusErr, err.Error())
			m.loadProfiles(name)
		} else {
			m.setStatus(statusOK, "Deleted "+name)
			m.loadProfiles("")
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

// finishAdd stores the wizard's endpoint/key/model as a binding. Adding also activates
// it. Editing an inactive binding only updates the catalog; editing the active one
// re-renders it. A curated one-model list is stored explicitly so future switches
// reproduce the exact picker selection.
func (m model) finishAdd(name string) (tea.Model, tea.Cmd) {
	if m.wiz.windows == nil {
		m.wiz.windows = map[string]int{}
	}
	slugs := m.pickerModels()
	// The binding's initial model is the first checked id. The tool's own model
	// menu switches it later; the picker does not keep a separate default.
	if len(slugs) > 0 && (m.wiz.model == "" || !containsID(slugs, m.wiz.model)) {
		m.wiz.model = slugs[0]
	}
	if len(slugs) == 0 && m.wiz.model != "" {
		slugs = []string{m.wiz.model}
	}
	if len(slugs) == 0 {
		m.setStatus(statusErr, "a binding needs at least one model")
		return m, nil
	}
	curated := len(m.wiz.models) > 0

	var existing *catalog.Binding
	if m.wiz.edit {
		b, found, err := m.cat.BindingByName(m.tool.Name, m.wiz.origName)
		if err != nil || !found {
			m.setStatus(statusErr, "no binding named "+m.wiz.origName)
			return m, nil
		}
		existing = &b
	}
	manualWindows := map[string]int{}
	for slug := range m.wiz.manualWindows {
		manualWindows[slug] = m.wiz.windows[slug]
	}
	b, err := catalog.StoreBinding(m.cat, m.tool, existing, name, m.wiz.endpoint, m.wiz.key, m.wiz.model, slugs, curated || !m.wiz.edit, manualWindows, m.wiz.piAPI)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}

	verb := "Added"
	if m.wiz.edit {
		verb = "Updated"
		if _, err := m.cat.ProjectIfActive(b.ID); err != nil {
			m.setStatus(statusErr, err.Error())
			return m, nil
		}
	} else if _, err := m.cat.Activate(b.ID); err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}

	m.setStatus(statusOK, fmt.Sprintf("%s %s (%s · %s)", verb, name, m.wiz.endpoint, describeModels(m.wiz.model, slugs)))
	m.formBaselineSet = false
	m.discardArmed = false
	m.view = viewProfiles
	m.loadProfiles(name)
	return m, nil
}

// parseContextWindow parses a token count or unit string (e.g. "1m", "200k", "1048576").
func parseContextWindow(val string) (int, error) {
	s := strings.ToLower(strings.TrimSpace(val))
	if s == "" {
		return 0, nil
	}
	multiplier := 1
	if strings.HasSuffix(s, "m") {
		multiplier = 1_000_000
		s = strings.TrimSuffix(s, "m")
	} else if strings.HasSuffix(s, "k") {
		multiplier = 1_000
		s = strings.TrimSuffix(s, "k")
	}
	s = strings.TrimSpace(s)
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil || f <= 0 {
		return 0, fmt.Errorf("context window must be a positive whole number (e.g. 1m, 200k, 1048576)")
	}
	window := int(f * float64(multiplier))
	if window <= 0 {
		return 0, fmt.Errorf("context window must be a positive whole number")
	}
	return window, nil
}

// parseModelIDs splits a pasted list into ids. Commas and newlines separate
// entries; nothing else is parsed, so an id that contains a colon (llama3:8b) or
// an @ (org/model@tag) survives intact. Context windows are edited in the list,
// never typed into the same field.
func parseModelIDs(val string) []string {
	var ids []string
	fields := strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == '\n' })
	for _, raw := range fields {
		if id := strings.TrimSpace(raw); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// addModels appends ids the list does not already hold and seeds each new row
// with its derived context window. It returns how many rows were added.
func (m *model) addModels(ids []string) int {
	if m.wiz.windows == nil {
		m.wiz.windows = map[string]int{}
	}
	if m.wiz.manualWindows == nil {
		m.wiz.manualWindows = map[string]bool{}
	}
	added := 0
	for _, id := range ids {
		if containsID(m.wiz.models, id) {
			continue
		}
		m.wiz.models = append(m.wiz.models, id)
		if _, ok := m.wiz.windows[id]; !ok {
			m.wiz.windows[id] = models.DefaultContextWindow(id)
		}
		added++
	}
	if m.wiz.model == "" && len(m.wiz.models) > 0 {
		m.wiz.model = m.wiz.models[0]
	}
	return added
}

// indexOfID returns the position of id in ids, or -1.
func indexOfID(ids []string, id string) int {
	for i, s := range ids {
		if s == id {
			return i
		}
	}
	return -1
}

// containsID reports whether id is already in ids.
func containsID(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
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

// describeModels summarizes a binding's model choice for the footer.
func describeModels(slug string, slugs []string) string {
	if slug == "" {
		return "no model"
	}
	if extra := len(slugs) - 1; extra > 0 {
		return fmt.Sprintf("%s +%d more in the tool's picker", slug, extra)
	}
	return slug
}
