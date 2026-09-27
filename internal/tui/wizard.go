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
	case viewReviewModels:
		return 3, 4, "review models"
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

// loadEditForm populates the native multi-input form.
func (m *model) loadEditForm() {
	m.formFocus = focusName
	m.formInputs = make([]textinput.Model, formInputCount)
	m.formInputs[focusName] = newFormInput("e.g. openrouter-fast", m.wiz.name, false)
	m.formInputs[focusURL] = newFormInput(exampleEndpoint, m.wiz.endpoint, false)
	m.formInputs[focusToken] = newFormInput("sk-or-v1-xxxxxxxx", m.wiz.key, false)
	modelPlaceholder := "e.g. gpt-4o (leave blank to use the first selected model)"
	if m.tool != nil && m.tool.Name == "claude" {
		modelPlaceholder = "e.g. claude-3-7-sonnet (leave blank to use the first selected model)"
	}
	m.formInputs[focusModel] = newFormInput(modelPlaceholder, m.wiz.modelField(), false)
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
	name := strings.TrimSpace(m.formInputs[focusName].Value())
	if name == "" {
		m.setStatus(statusErr, "Name is required")
		return m, nil
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
			m.setStatus(statusErr, "name is required")
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
			m.loadModels()
			return m, nil
		}
		if m.view == viewModelWindow {
			m.view = viewModels
			m.clearStatus()
			m.loadModels()
			return m, nil
		}
		if m.view == viewEditForm && m.editField != "" {
			m.editField = ""
			m.loadEditForm()
			return m, nil
		}
		if m.view == viewEditField {
			if m.fromReview {
				m.fromReview = false
				m.view = viewReviewModels
				m.editTarget = ""
				return m, nil
			}
			m.view = viewPickModel // cancel window editing → back to the picker
			m.editTarget = ""
			m.renderModels()
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
				m.loadEditFormAt(focusManual)
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
				slug := m.editTarget
				m.editTarget = ""
				if m.cat != nil {
					if p, found, err := m.cat.ProviderByURL(m.tool.ResolveEndpoint(m.wiz.endpoint)); err == nil && found {
						if _, err := m.cat.PutModelWithWindow(p.ID, slug, window); err != nil {
							m.setStatus(statusErr, err.Error())
							return m, nil
						}
					}
				}
				if m.wiz.windows == nil {
					m.wiz.windows = map[string]int{}
				}
				m.wiz.windows[slug] = window
				if m.fromReview {
					m.fromReview = false
					m.view = viewReviewModels
					m.clearStatus()
					if window == 0 {
						m.setStatus(statusOK, "cleared context window for "+slug)
					} else {
						m.setStatus(statusOK, fmt.Sprintf("saved context window for %s: %d", slug, window))
					}
					return m, nil
				}
				m.view = viewPickModel
				m.clearStatus()
				m.showLocalModels()
				m.list.Select(indexOfValue(m.list.Items(), slug))
				if window == 0 {
					m.setStatus(statusOK, "cleared context window for "+slug)
				} else {
					m.setStatus(statusOK, fmt.Sprintf("saved context window for %s: %d", slug, window))
				}
				return m, nil
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

		case viewAddCustomModel:
			parsed, err := parseTypedModelSpecs(val)
			if err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			if len(parsed) == 0 {
				m.setStatus(statusErr, "enter at least one model id")
				return m, nil
			}
			p, err := m.cat.PutProvider(m.tool.ResolveEndpoint(m.wiz.endpoint))
			if err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			slugs := make([]string, 0, len(parsed))
			if m.wiz.windows == nil {
				m.wiz.windows = map[string]int{}
			}
			for _, pm := range parsed {
				slugs = append(slugs, pm.Slug)
				m.wiz.windows[pm.Slug] = pm.Window
				if pm.Manual {
					if _, err := m.cat.PutModelWithWindow(p.ID, pm.Slug, pm.Window); err != nil {
						m.setStatus(statusErr, err.Error())
						return m, nil
					}
				} else {
					if _, err := m.cat.PutModel(p.ID, pm.Slug); err != nil {
						m.setStatus(statusErr, err.Error())
						return m, nil
					}
				}
			}
			m.wiz.models = append([]string(nil), slugs...)
			m.wiz.model = slugs[0]
			m.clearStatus()
			m.view = viewReviewModels
			m.reviewSource = viewAddCustomModel
			m.reviewCursor = 0
			m.setStatus(statusOK, "models loaded — enter to confirm, w to edit context, d to remove")
			return m, nil

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
			m.view = viewModelWindow
			m.clearStatus()
			m.startInput("context window in tokens (empty = unknown)", false)
			if m.editingModel.ID != "" && m.editingModel.ContextWindow > 0 {
				m.input.SetValue(fmt.Sprint(m.editingModel.ContextWindow))
			}
			return m, textinput.Blink

		case viewModelWindow:
			window := 0
			if strings.TrimSpace(val) != "" {
				var err error
				if window, err = parseContextWindow(val); err != nil {
					m.setStatus(statusErr, err.Error())
					return m, nil
				}
			}
			p, err := m.cat.PutProvider(m.modelEditEndpoint)
			if err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			saved, err := m.cat.PutModelWithWindow(p.ID, m.editingModel.Slug, window)
			if err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			if m.editingModel.ID != "" && m.editingModel.ID != saved.ID {
				if err := m.cat.RemoveModel(m.editingModel.ID); err != nil {
					m.setStatus(statusErr, err.Error())
					m.view = viewModels
					m.loadModels()
					return m, nil
				}
			}
			m.editingModel = catalog.Model{}
			m.modelEditEndpoint = ""
			m.view = viewModels
			m.loadModels()
			m.setStatus(statusOK, "Saved "+saved.Slug)
			return m, nil

		case viewDupName:
			val = strings.TrimSpace(val)
			if val == "" {
				m.setStatus(statusErr, "name is required")
				return m, nil
			}
			src := m.dupSource
			if err := m.cloneBinding(src, val); err != nil {
				m.setStatus(statusErr, err.Error())
				return m, nil
			}
			m.dupSource = ""
			m.view = viewProfiles
			m.setStatus(statusOK, "Duplicated "+src+" → "+val)
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
	if catalog.SingleModelTools[m.tool.Name] && len(slugs) > 1 {
		slugs = slugs[:1]
		m.wiz.model = slugs[0]
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
	b, err := catalog.StoreBinding(m.cat, m.tool, existing, name, m.wiz.endpoint, m.wiz.key, m.wiz.model, slugs, curated || !m.wiz.edit, manualWindows)
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
	m.view = viewProfiles
	m.loadProfiles(name)
	return m, nil
}

// cloneBinding copies a binding under a new name, sharing its credential and models.
func (m model) cloneBinding(src, _ string) error {
	return m.copyBindingToTool(src, m.tool.Name)
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

type parsedModelSpec struct {
	Slug   string
	Window int
	Manual bool
}

func parseTypedModelSpecs(val string) ([]parsedModelSpec, error) {
	var specs []parsedModelSpec
	for _, raw := range strings.Split(val, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var slug string
		var window int
		var manual bool
		if idx := strings.IndexAny(raw, ":@"); idx >= 0 {
			slug = strings.TrimSpace(raw[:idx])
			wStr := strings.TrimSpace(raw[idx+1:])
			if slug == "" {
				return nil, fmt.Errorf("missing model id before %q", string(raw[idx]))
			}
			w, err := parseContextWindow(wStr)
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", slug, err)
			}
			window = w
			manual = true
		} else {
			slug = raw
			window = models.DefaultContextWindow(slug)
		}
		specs = append(specs, parsedModelSpec{Slug: slug, Window: window, Manual: manual})
	}
	return specs, nil
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
// A tool that can hold only one model (Codex) never gets more than the chosen slug.
func (m model) pickerModels() []string {
	if m.tool != nil && catalog.SingleModelTools[m.tool.Name] {
		if m.wiz.model != "" {
			return []string{m.wiz.model}
		}
		if len(m.wiz.models) > 0 {
			return m.wiz.models[:1]
		}
		if len(m.allModels) > 0 {
			return m.allModels[:1]
		}
		return nil
	}
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
