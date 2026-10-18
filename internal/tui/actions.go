package tui

import (
	"fmt"
	"strings"

	"charon/internal/catalog"
	"charon/internal/secret"
	"charon/internal/tools"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// editContextFromList opens the context-window editor for the highlighted
// model-library row (the "e"/"w" action on the models screen).
func (m model) editContextFromList() (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(item)
	if !ok || isSentinel(it.value) {
		return m, nil
	}
	m.view = viewModelWindow
	if err := m.editModelWindowByID(it.value); err != nil {
		m.setStatus(statusErr, err.Error())
		m.view = viewModels
		m.loadModels(it.value)
		return m, nil
	}
	return m, textinput.Blink
}

// onEditKey opens the edit form for the highlighted binding ("e").
func (m model) onEditKey() (tea.Model, tea.Cmd) {
	it, ok := m.selectedBinding()
	if !ok {
		return m, nil
	}
	b, found, err := m.cat.BindingByName(m.tool.Name, it.value)
	if err != nil || !found {
		m.setStatus(statusErr, "no binding named "+it.value)
		return m, nil
	}
	cr, err := m.cat.Credential(b.CredentialID)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	p, err := m.cat.Provider(cr.ProviderID)
	if err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	slug, _ := m.cat.ModelSlug(b.ModelID)
	slugs, _ := m.cat.ModelSlugs(b.Models)
	windows := make(map[string]int, len(b.Models))
	for _, id := range b.Models {
		if m, err := m.cat.Model(id); err == nil {
			windows[m.Slug] = m.ContextWindow
		}
	}
	m.wiz = wizard{name: it.value, origName: it.value, edit: true,
		endpoint: p.BaseURL, key: cr.Key, model: slug, models: slugs, windows: windows, piAPI: b.PiAPI}
	m.editField = ""
	m.view = viewEditForm
	m.clearStatus()
	m.loadEditForm()
	return m, nil
}

// onHideKey toggles whether the highlighted binding belongs to the tool's
// shared endpoint list ("h"). Hiding keeps the binding here — name, key, models,
// protocol — and rewrites the active binding's config so the endpoint drops out
// of the tool's picker. The active binding itself cannot be hidden: its defaults
// would point at an entry that is no longer written.
func (m model) onHideKey() (tea.Model, tea.Cmd) {
	it, ok := m.selectedBinding()
	if !ok {
		return m, nil
	}
	b, found, err := m.cat.BindingByName(m.tool.Name, it.value)
	if err != nil || !found {
		m.setStatus(statusErr, "no binding named "+it.value)
		return m, nil
	}
	hide := !b.Hidden
	if err := m.cat.SetBindingHidden(b.ID, hide); err != nil {
		m.setStatus(statusErr, err.Error())
		return m, nil
	}
	status := "Showed " + it.value
	if hide {
		status = "Hid " + it.value
	}
	// The list lives in the active binding's config, so re-render it for the
	// change to reach the tool. Without an active binding there is nothing
	// written to update; the next switch picks the flag up.
	if active, found, err := m.cat.Active(m.tool.Name); err == nil && found {
		if _, err := m.cat.Reapply(active.ID); err != nil {
			m.setStatus(statusErr, status+" in charon, but rewriting the tool config failed: "+err.Error())
			m.loadProfiles(it.value)
			return m, nil
		}
		if note := aggregationNote(m.tool, m.cat); note != "" {
			status += " · " + note
		}
	}
	m.setStatus(statusOK, status)
	m.loadProfiles(it.value)
	return m, nil
}

// onDeleteKey arms the confirm-delete prompt for the highlighted binding ("d").
// The active binding is not deletable; switch away first.
func (m model) onDeleteKey() (tea.Model, tea.Cmd) {
	it, ok := m.selectedBinding()
	if !ok {
		return m, nil
	}
	if active, found, err := m.cat.Active(m.tool.Name); err == nil && found && active.Name == it.value {
		m.setStatus(statusErr, fmt.Sprintf("cannot delete active binding %q; switch to another binding first", it.value))
		return m, nil
	}
	m.delTarget = it.value
	m.showConfirm = true
	m.clearStatus()
	return m, nil
}

// startDuplicate opens the destination chooser for the highlighted binding
// ("c" or "x"). The first row duplicates within the current tool; the rest copy
// to another tool. A naming step follows either way, so no "-copy" suffix is
// ever written without the user seeing it first.
func (m model) startDuplicate(name string) (tea.Model, tea.Cmd) {
	if _, found, err := m.cat.BindingByName(m.tool.Name, name); err != nil || !found {
		m.setStatus(statusErr, "no binding named "+name)
		return m, nil
	}
	m.dupSource = name
	m.dupTool = ""
	m.view = viewCopyTool
	m.clearStatus()
	m.loadDuplicateTools()
	return m, nil
}

// duplicateBinding copies the binding srcName into dstToolName under newName.
// Within the same tool the copy shares the credential row — that is the point of
// duplicating, and why the first destination row exists. Across tools
// StoreBinding writes a fresh provider/credential pair, reusing an existing row
// only when the endpoint and key match one exactly. Pi's request protocol
// survives only pi→pi; other tools have no such setting. The duplicate is never
// activated.
func (m model) duplicateBinding(srcName, dstToolName, newName string) error {
	dst := m.findTool(dstToolName)
	if dst == nil {
		return fmt.Errorf("unknown tool %s", dstToolName)
	}
	b, found, err := m.cat.BindingByName(m.tool.Name, srcName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no binding named %s", srcName)
	}
	slug, err := m.cat.ModelSlug(b.ModelID)
	if err != nil {
		return err
	}
	slugs, err := m.cat.ModelSlugs(b.Models)
	if err != nil {
		return err
	}
	if dst.Name == m.tool.Name {
		dup, err := m.cat.AddBinding(dst.Name, newName, b.CredentialID, slug, slugs, b.PiAPI)
		if err != nil {
			return err
		}
		return m.hideDuplicate(dup.ID, dst, b.Hidden)
	}
	cr, err := m.cat.Credential(b.CredentialID)
	if err != nil {
		return err
	}
	p, err := m.cat.Provider(cr.ProviderID)
	if err != nil {
		return err
	}
	piAPI := ""
	if dst.Name == "pi" && b.Tool == "pi" {
		piAPI = b.PiAPI
	}
	dup, err := catalog.StoreBinding(m.cat, dst, nil, newName, p.BaseURL, cr.Key, slug, slugs, true, nil, piAPI)
	if err != nil {
		return err
	}
	return m.hideDuplicate(dup.ID, dst, b.Hidden)
}

// hideDuplicate carries the source's hidden flag onto a copy, so duplicating a
// hidden binding does not quietly put the endpoint back in the tool's list. A
// destination tool without an endpoint list has nothing to hide from.
func (m model) hideDuplicate(id string, dst *tools.Tool, hidden bool) error {
	if !hidden || dst == nil || !dst.MultiEndpoint {
		return nil
	}
	return m.cat.SetBindingHidden(id, true)
}

// nextDuplicateName returns the first free "<src>-copy" (then "<src>-copy-2", …).
func nextDuplicateName(existing []string, src string) string {
	taken := make(map[string]bool, len(existing))
	for _, n := range existing {
		taken[n] = true
	}
	base := src + "-copy"
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !taken[cand] {
			return cand
		}
	}
}

// skipSeparators nudges the cursor off inert rows — the divider, the table's
// column labels, and drift annotations — after a move, continuing in the
// direction of travel so the gap never traps the selection.
func (m *model) skipSeparators(before int) {
	down := m.list.Index() >= before
	for range m.list.Items() {
		it, ok := m.list.SelectedItem().(item)
		if !ok || !inertSentinel(it.value) {
			return
		}
		if down {
			m.list.CursorDown()
		} else {
			m.list.CursorUp()
		}
	}
}

// startInput configures the text field for a step (password masks the echo).
func (m *model) startInput(placeholder string, password bool) {
	m.input.SetValue("")
	m.input.Placeholder = placeholder
	if password {
		m.input.EchoMode = textinput.EchoPassword
	} else {
		m.input.EchoMode = textinput.EchoNormal
	}
	m.input.Focus()
}

func (m model) onEsc() (tea.Model, tea.Cmd) {
	switch m.view {
	case viewTools:
		return m, tea.Quit
	case viewProfiles:
		m.view = viewTools
		m.clearStatus()
		m.loadTools()
		m.resize() // banner returns → shrink the list
		return m, nil
	case viewModels:
		m.view = viewTools
		m.clearStatus()
		m.loadTools()
		m.resize()
		return m, nil
	case viewCopyTool:
		src := m.dupSource
		m.dupSource = ""
		m.dupTool = ""
		m.view = viewProfiles
		m.clearStatus()
		m.loadProfiles(src)
		return m, nil
	case viewEditForm:
		m.editField = ""
		m.dupSource = ""
		m.dupTool = ""
		m.view = viewProfiles
		m.setStatus(statusInfo, "cancelled")
		m.loadProfiles("")
		return m, nil
	case viewPickModel:
		m.view = viewEditForm
		m.loadEditFormAt(focusFetch)
		return m, nil
	}
	return m, nil
}

func (m model) onEnter() (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		return m, nil
	}
	if inertSentinel(it.value) {
		return m, nil // divider, column labels, and drift annotations are inert
	}
	switch m.view {
	case viewTools:
		if it.value == libSentinel {
			m.view = viewModels
			m.clearStatus()
			m.modelFilter = "" // a fresh visit starts unfiltered
			m.loadModels("")
			m.resize()
			return m, nil
		}
		t := m.findTool(it.value)
		if t == nil || t.Detected == nil || !t.Detected() {
			m.setStatus(statusInfo, it.title+" isn't installed yet — see the README to set it up")
			return m, nil
		}
		m.tool = t
		m.view = viewProfiles
		m.clearStatus()
		m.loadProfiles("") // land on the active binding
		m.resize()         // banner hidden → grow the list
		return m, nil

	case viewCopyTool:
		// Destination chosen: name the duplicate before anything is written.
		m.dupTool = it.value
		m.view = viewDupName
		existing, err := m.cat.Bindings(it.value)
		if err != nil {
			m.setStatus(statusErr, err.Error())
			return m, nil
		}
		names := make([]string, len(existing))
		for i, b := range existing {
			names[i] = b.Name
		}
		m.startInput("name", false)
		m.input.SetValue(nextDuplicateName(names, m.dupSource))
		m.clearStatus()
		return m, textinput.Blink

	case viewModels:
		if it.value == addSentinel {
			m.editingModel = catalog.Model{}
			m.modelEditEndpoint = ""
			m.view = viewModelEndpoint
			m.clearStatus()
			m.startInput("endpoint, e.g. https://openrouter.ai/api/v1", false)
			return m, textinput.Blink
		}
		m.view = viewModelWindow
		if err := m.editModelWindowByID(it.value); err != nil {
			m.setStatus(statusErr, err.Error())
			m.view = viewModels
			m.loadModels(it.value)
			return m, nil
		}
		return m, textinput.Blink

	case viewProfiles:
		if it.value == addSentinel {
			m.wiz = wizard{}
			m.view = viewEditForm
			m.loadEditForm()
			return m, nil
		}
		b, found, err := m.cat.BindingByName(m.tool.Name, it.value)
		if err != nil || !found {
			m.setStatus(statusErr, "no binding named "+it.value)
			return m, nil
		}
		if active, found, err := m.cat.Active(m.tool.Name); err == nil && found && active.ID == b.ID {
			m.setStatus(statusInfo, it.value+" is already active")
			return m, nil
		}
		if _, err := m.cat.Activate(b.ID); err != nil {
			m.setStatus(statusErr, err.Error())
		} else {
			info, _ := m.tool.Describe()
			status := fmt.Sprintf("Switched to %s (%s · %s)",
				it.value, info.Endpoint, secret.Mask(info.Secret))
			if note := aggregationNote(m.tool, m.cat); note != "" {
				status += " · " + note
			}
			m.setStatus(statusOK, status)
			m.loadProfiles(it.value)
		}

	case viewEditForm:
		// "e" or enter edits the highlighted field; esc saves & backs out.
		it, ok := m.list.SelectedItem().(item)
		if ok {
			return m.onEditFormSelect(it.value)
		}
		return m, nil

	}
	return m, nil
}

func (m model) updateReviewModels(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyUp:
		if m.reviewCursor > 0 {
			m.reviewCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.reviewCursor < len(m.wiz.models)-1 {
			m.reviewCursor++
		}
		return m, nil
	case tea.KeyEsc:
		if m.reviewSource == viewPickModel {
			m.view = viewPickModel
			m.clearStatus()
			m.renderModels()
			return m, nil
		}
		m.view = viewAddCustomModel
		m.clearStatus()
		m.startInput("Type model IDs separated by commas, optionally with context (e.g. glm-5:1m, kimi-k3, custom:200k)", false)
		m.input.SetValue(strings.Join(m.wiz.models, ", "))
		return m, textinput.Blink
	case tea.KeyEnter:
		if len(m.wiz.models) == 0 {
			m.setStatus(statusErr, "at least one model is required")
			return m, nil
		}
		m.wiz.model = m.wiz.models[0]
		if m.fromForm || m.wiz.edit {
			m.fromForm = false
			m.editField = fieldModel
			m.view = viewEditForm
			focusPos := focusManual
			if m.reviewSource == viewPickModel {
				focusPos = focusFetch
			}
			m.loadEditFormAt(focusPos)
			return m, nil
		}
		m.view = viewAddName
		m.startInput("binding name (e.g. openrouter-fast)", false)
		return m, textinput.Blink
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'k', 'K':
				if m.reviewCursor > 0 {
					m.reviewCursor--
				}
				return m, nil
			case 'j', 'J':
				if m.reviewCursor < len(m.wiz.models)-1 {
					m.reviewCursor++
				}
				return m, nil
			case 'w', 'W', 'e', 'E':
				if len(m.wiz.models) == 0 {
					return m, nil
				}
				if m.reviewCursor >= len(m.wiz.models) {
					m.reviewCursor = 0
				}
				target := m.wiz.models[m.reviewCursor]
				m.editTarget = target
				m.editField = ""
				m.fromReview = true
				m.view = viewEditField
				m.startInput("context window in tokens (e.g. 1m, 200k, 1048576, empty=unknown)", false)
				if win, ok := m.wiz.windows[target]; ok && win > 0 {
					m.input.SetValue(fmt.Sprint(win))
				} else {
					m.input.SetValue("")
				}
				return m, textinput.Blink
			case 'd', 'D':
				if len(m.wiz.models) <= 1 {
					m.setStatus(statusErr, "at least one model is required")
					return m, nil
				}
				if m.reviewCursor >= len(m.wiz.models) {
					m.reviewCursor = 0
				}
				removed := m.wiz.models[m.reviewCursor]
				var kept []string
				for i, s := range m.wiz.models {
					if i != m.reviewCursor {
						kept = append(kept, s)
					}
				}
				m.wiz.models = kept
				if m.reviewCursor >= len(m.wiz.models) {
					m.reviewCursor = len(m.wiz.models) - 1
				}
				m.setStatus(statusOK, "removed "+removed)
				return m, nil
			}
		}
	}
	return m, nil
}
