package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case fetchedMsg:
		// Hold a too-fast result until minLoadDuration so the loading screen never flickers.
		if elapsed := time.Since(m.fetchStart); elapsed < minLoadDuration {
			m.pending = &msg
			return m, tea.Tick(minLoadDuration-elapsed, func(time.Time) tea.Msg { return minLoadElapsedMsg{} })
		}
		return m.applyFetched(msg)

	case minLoadElapsedMsg:
		if m.pending == nil {
			return m, nil
		}
		result := *m.pending
		m.pending = nil
		return m.applyFetched(result)

	case statusTickMsg:
		if m.status != "" && m.statusAt.Equal(msg.at) {
			m.status = ""
			m.statusLvl = statusInfo
		}
		return m, nil

	case spinner.TickMsg:
		if m.view != viewFetching {
			return m, nil // ignore stray ticks once we've left the loading screen
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		// Lazy expiry: the scheduled tick clears an idle screen's status, and
		// this catches statuses set on paths that never arm one — any keypress
		// sweeps a message older than its TTL.
		if m.status != "" && time.Since(m.statusAt) > statusTTL {
			m.clearStatus()
		}
		// ctrl+c is the only way to quit, from any screen.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.view == viewEditForm {
			return m.updateEditForm(msg)
		}
		if m.inputView() {
			return m.updateInput(msg)
		}
		if m.showConfirm {
			return m.handleConfirmDelete(msg)
		}
		if m.view == viewPickModel {
			return m.updatePickModel(msg)
		}
		if m.view == viewReviewModels {
			return m.updateReviewModels(msg)
		}
		// While the model library is being searched, letters belong to the
		// query: the screen's own shortcuts (q/e/w/r/d) only fire before a
		// search has started — the same rule the picker follows.
		searching := m.view == viewModels && m.modelFilter != ""
		switch msg.String() {
		case "esc":
			if m.view == viewModels && m.modelFilter != "" {
				m.modelFilter = "" // first esc clears the search, the next one leaves
				m.loadModels("")
				return m, nil
			}
			return m.onEsc()
		case "q":
			if searching {
				break // the letter belongs to the query
			}
			if m.view == viewTools || m.view == viewProfiles || m.view == viewModels {
				return m.onEsc()
			}
		case "enter":
			return m.onEnter()
		case "ctrl+r":
			if m.view == viewProfiles {
				selected := ""
				if it, ok := m.list.SelectedItem().(item); ok {
					selected = it.value
				}
				m.loadProfiles(selected)
				m.setStatus(statusInfo, "Refreshed on-disk config; nothing written")
				return m, nil
			}
		case "r":
			if searching {
				break // the letter belongs to the query
			}
			if m.view == viewProfiles {
				it, ok := m.selectedBinding()
				if !ok {
					return m, nil
				}
				b, found, err := m.cat.BindingByName(m.tool.Name, it.value)
				if err != nil || !found {
					m.setStatus(statusErr, "no binding named "+it.value)
					return m, nil
				}
				if _, err := m.cat.Reapply(b.ID); err != nil {
					m.setStatus(statusErr, err.Error())
				} else {
					status := "Reapplied " + it.value
					if note := aggregationNote(m.tool, m.cat); note != "" {
						status += " · " + note
					}
					m.setStatus(statusOK, status)
					m.loadProfiles(it.value)
				}
				return m, nil
			}
			if m.view == viewModels {
				if it, ok := m.list.SelectedItem().(item); ok && !isSentinel(it.value) {
					m.view = viewModelEffort
					if err := m.editModelEffortByID(it.value); err != nil {
						m.setStatus(statusErr, err.Error())
						m.view = viewModels
						m.loadModels(it.value)
						return m, nil
					}
					return m, textinput.Blink
				}
			}
		case "a":
			if m.view == viewProfiles {
				m.wiz = wizard{windows: map[string]int{}}
				m.view = viewEditForm
				m.clearStatus()
				m.loadEditForm()
				return m, nil
			}
		case "e":
			if searching {
				break // the letter belongs to the query
			}
			if m.view == viewEditForm {
				// Inside the edit form, "e" opens the highlighted field for editing.
				if it, ok := m.list.SelectedItem().(item); ok {
					return m.onEditFormSelect(it.value)
				}
				return m, nil
			}
			if m.view == viewProfiles && m.tool.ApplyAuth != nil {
				return m.onEditKey()
			}
			if m.view == viewModels {
				return m.editContextFromList()
			}
		case "w":
			// Same as "e" here: w is context everywhere else, keep it so on this screen.
			if searching {
				break // the letter belongs to the query
			}
			if m.view == viewModels {
				return m.editContextFromList()
			}
		case "c", "x":
			if m.view == viewProfiles {
				if it, ok := m.selectedBinding(); ok {
					return m.startDuplicate(it.value)
				}
				return m, nil
			}
		case "h":
			if m.view == viewProfiles {
				return m.onHideKey()
			}
		case "d":
			if searching {
				break // the letter belongs to the query
			}
			if m.view == viewProfiles {
				return m.onDeleteKey()
			}
			if m.view == viewModels {
				if it, ok := m.list.SelectedItem().(item); ok && !isSentinel(it.value) {
					curIdx := m.list.Index()
					if err := m.cat.RemoveModel(it.value); err != nil {
						m.setStatus(statusErr, err.Error())
						m.loadModels(it.value)
					} else {
						m.setStatus(statusOK, "Deleted "+it.title)
						m.loadModels("", curIdx)
					}
				}
				return m, nil
			}
		}
	}
	// On the model library, any letter that is not a screen shortcut types a
	// search; the switch above owns the shortcuts, so typing can only start on
	// a free key (same rule as the picker's search).
	if m.view == viewModels && !m.showConfirm {
		if key, ok := msg.(tea.KeyMsg); ok && m.typeModelSearch(key) {
			if tick := m.statusTick(); tick != nil {
				return m, tick
			}
			return m, nil
		}
	}
	before := m.list.Index()
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	if m.view == viewProfiles || m.view == viewModels {
		m.skipSeparators(before)
	}
	if tick := m.statusTick(); tick != nil {
		return m, tea.Batch(cmd, tick)
	}
	return m, cmd
}

// typeModelSearch feeds the model library's search: printable keys extend the
// query, backspace trims it. Returns false for every other key so the caller
// falls through to the list.
func (m *model) typeModelSearch(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes:
		m.modelFilter += string(msg.Runes)
		m.loadModels("")
		return true
	case tea.KeyBackspace:
		if r := []rune(m.modelFilter); len(r) > 0 {
			m.modelFilter = string(r[:len(r)-1])
			m.loadModels("")
			return true
		}
	}
	return false
}
