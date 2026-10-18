// Package tui provides the interactive arrow-key menu for charon.
//
// The model is split across a few files in this package:
//   - tui.go     the entry point (Run)
//   - model.go   the model type, view/status vocabulary and construction
//   - load.go    loading each screen's rows (tools, bindings, models)
//   - update.go  the message loop: Init, Update and search typing
//   - actions.go the key shortcuts acting on the highlighted row
//   - views.go   rendering (View, wizard header, prompts, status line)
//   - wizard.go  the add/edit binding flow and confirm-delete prompt
//   - picker.go  the fetch-and-choose-a-model screen
package tui

import (
	"charon/internal/catalog"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the interactive menu against the catalog.
func Run(cat *catalog.Catalog, version string) error {
	_, err := tea.NewProgram(newModel(cat, version), tea.WithAltScreen()).Run()
	return err
}
