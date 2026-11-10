// Package statusbar renders contextual keyboard shortcuts and status messages
// along the bottom edge of the TUI.
package statusbar

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/tkilb/lazyprint/internal/ui/panel"
)

// PanelID indicates the currently focused panel in the grid.
type PanelID int

const (
	PanelDetails  PanelID = 0
	PanelPrinters PanelID = 1
	PanelJobs     PanelID = 2
	PanelScanners PanelID = 3
)

// Model holds statusbar state.
type Model struct {
	Width   int
	Message string
	IsError bool
}

// New returns a new statusbar Model.
func New() Model {
	return Model{}
}

// SetMessage sets a temporary status or feedback message.
func (m *Model) SetMessage(msg string, isError bool) {
	m.Message = msg
	m.IsError = isError
}

// ClearMessage clears the current message.
func (m *Model) ClearMessage() {
	m.Message = ""
	m.IsError = false
}

// View renders the status line across m.Width.
func (m Model) View(focus PanelID) string {
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	pair := func(key, desc string) string {
		return keyStyle.Render(key) + descStyle.Render(":"+desc)
	}

	var contextual []string
	switch focus {
	case PanelPrinters:
		contextual = []string{
			pair("j/k", "nav"),
			pair("d", "default"),
			pair("R", "rename"),
			pair("x", "delete"),
			pair("enter", "print"),
		}
	case PanelJobs:
		contextual = []string{
			pair("j/k", "nav"),
			pair("x", "cancel"),
			pair("X", "purge"),
			pair("h", "hold"),
		}
	case PanelScanners:
		contextual = []string{
			pair("j/k", "nav"),
			pair("s", "scan"),
			pair("f", "flatbed"),
			pair("a", "adf"),
		}
	case PanelDetails:
		contextual = []string{
			pair("j/k", "scroll"),
		}
	}

	globals := []string{
		pair("0-3", "panel"),
		pair("tab", "cycle"),
		pair("r", "refresh"),
		pair("q", "quit"),
	}

	left := strings.Join(append(contextual, globals...), sepStyle.Render("  "))

	if m.Message != "" {
		msgColor := panel.FocusedColor
		if m.IsError {
			msgColor = panel.ErrorColor
		}
		msg := lipgloss.NewStyle().Bold(true).Foreground(msgColor).Render(m.Message)
		if lipgloss.Width(left)+lipgloss.Width(msg)+4 < m.Width {
			padding := m.Width - lipgloss.Width(left) - lipgloss.Width(msg) - 2
			if padding > 0 {
				left = left + strings.Repeat(" ", padding) + msg
			}
		} else {
			left = msg
		}
	}

	return lipgloss.NewStyle().
		Width(m.Width).
		MaxHeight(1).
		Padding(0, 1).
		Render(left)
}
