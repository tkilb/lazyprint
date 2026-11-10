// Package popup provides modal dialogs and overlays with severity styling parity to lazytask.
package popup

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Severity controls popup border/title color.
type Severity int

const (
	Info Severity = iota
	Warning
	Error
)

func (s Severity) String() string {
	switch s {
	case Warning:
		return "Warning"
	case Error:
		return "Error"
	default:
		return "Info"
	}
}

func (s Severity) color() lipgloss.Color {
	switch s {
	case Warning:
		return lipgloss.Color("214") // orange
	case Error:
		return lipgloss.Color("196") // red
	default:
		return lipgloss.Color("2") // green
	}
}

// Message is a queued popup message.
type Message struct {
	Severity Severity
	Text     string
}

// Model holds a FIFO queue of popup messages.
type Model struct {
	queue []Message
}

func (m Model) Push(msg Message) Model {
	m.queue = append(append([]Message{}, m.queue...), msg)
	return m
}

func (m Model) Active() bool {
	return len(m.queue) > 0
}

func (m Model) Current() (Message, bool) {
	if len(m.queue) == 0 {
		return Message{}, false
	}
	return m.queue[0], true
}

func (m Model) Dismiss() Model {
	if len(m.queue) == 0 {
		return m
	}
	m.queue = m.queue[1:]
	return m
}

const (
	minBoxWidth = 34
	maxBoxWidth = 62

	confirmChoice = "(y/enter) confirm"
	cancelChoice  = "(n/esc) cancel"
	choiceGap     = "   "
	confirmHint   = confirmChoice + choiceGap + cancelChoice

	boxContentInset = 2
)

// ConfirmChoiceAt checks if a click coordinate falls on confirm or cancel.
func ConfirmChoiceAt(x int) (confirm, ok bool) {
	x -= boxContentInset
	switch {
	case x >= 0 && x < len(confirmChoice):
		return true, true
	case x >= len(confirmChoice)+len(choiceGap) && x < len(confirmHint):
		return false, true
	}
	return false, false
}

// Box renders an info/warning/error message modal.
func Box(msg Message, screenWidth int) string {
	return renderBox(msg.Severity.String(), msg.Severity.color(), "press any key to dismiss", msg.Text, screenWidth)
}

// ConfirmBox renders a yes/no confirmation prompt.
func ConfirmBox(text string, screenWidth int) string {
	return renderBox("Confirm", lipgloss.Color("214"), confirmHint, text, screenWidth)
}

// DangerConfirmBox renders a red confirmation prompt for destructive actions.
func DangerConfirmBox(text string, screenWidth int) string {
	return renderBox("Confirm", lipgloss.Color("196"), confirmHint, text, screenWidth)
}

// PromptBox renders an interactive text input modal box (e.g. for rename).
func PromptBox(title, text, inputRendered string, screenWidth int) string {
	boxWidth := computeBoxWidth(screenWidth)
	color := lipgloss.Color("2")

	header := lipgloss.NewStyle().Bold(true).Foreground(color).Render(title)
	hint := lipgloss.NewStyle().Faint(true).Render("(enter) confirm   (esc) cancel")

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		text,
		"",
		inputRendered,
		"",
		hint,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Width(boxWidth).
		Render(body)
}

func computeBoxWidth(screenWidth int) int {
	boxWidth := maxBoxWidth
	if screenWidth > 0 && screenWidth-4 < boxWidth {
		boxWidth = screenWidth - 4
	}
	if boxWidth < minBoxWidth {
		boxWidth = minBoxWidth
	}
	return boxWidth
}

func renderBox(titleText string, color lipgloss.Color, hintText, bodyText string, screenWidth int) string {
	boxWidth := computeBoxWidth(screenWidth)

	title := lipgloss.NewStyle().Bold(true).Foreground(color).Render(titleText)
	hint := lipgloss.NewStyle().Faint(true).Render(hintText)

	body := lipgloss.JoinVertical(lipgloss.Left, title, "", bodyText, "", hint)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Width(boxWidth).
		Render(body)
}

// Overlay composites box centered on top of background.
func Overlay(background, box string, screenWidth, screenHeight int) string {
	if screenWidth <= 0 || screenHeight <= 0 {
		return box
	}

	bgLines := strings.Split(background, "\n")
	for len(bgLines) < screenHeight {
		bgLines = append(bgLines, "")
	}

	boxLines := strings.Split(box, "\n")
	boxWidth := 0
	for _, l := range boxLines {
		if w := ansi.StringWidth(l); w > boxWidth {
			boxWidth = w
		}
	}
	boxHeight := len(boxLines)

	startRow := (screenHeight - boxHeight) / 2
	startCol := (screenWidth - boxWidth) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	out := make([]string, len(bgLines))
	copy(out, bgLines)

	for i, boxLine := range boxLines {
		row := startRow + i
		if row < 0 || row >= len(out) {
			continue
		}
		bgLine := out[row]
		if w := ansi.StringWidth(bgLine); w < screenWidth {
			bgLine += strings.Repeat(" ", screenWidth-w)
		}

		if w := ansi.StringWidth(boxLine); w < boxWidth {
			boxLine += strings.Repeat(" ", boxWidth-w)
		}

		left := ansi.Cut(bgLine, 0, startCol)
		right := ansi.Cut(bgLine, startCol+boxWidth, screenWidth)
		out[row] = left + boxLine + right
	}

	return strings.Join(out, "\n")
}
