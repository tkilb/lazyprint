// Package panel provides bordered panel rendering with embedded top-border titles
// and palette parity with lazytask.
package panel

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	paletteDefault = lipgloss.Color("")
	paletteGreen   = lipgloss.Color("2")   // Focused border / title
	paletteBlue    = lipgloss.Color("4")   // Active row selection
	paletteOrange  = lipgloss.Color("214") // Warning
	paletteRed     = lipgloss.Color("196") // Error
)

const (
	UnfocusedColor        = paletteDefault
	FocusedColor          = paletteGreen
	SelectedRowBackground = paletteBlue
	WarningColor          = paletteOrange
	ErrorColor            = paletteRed

	minWidth  = 10
	minHeight = 1
)

// SelectedRowStyle highlights the currently selected row in a list panel.
func SelectedRowStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Background(SelectedRowBackground).
		Foreground(lipgloss.Color("15")).
		Bold(true).
		Width(width)
}

// TitleStyle returns the bold title style colored by focus.
func TitleStyle(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(borderColor(focused))
}

func borderColor(focused bool) lipgloss.Color {
	if focused {
		return FocusedColor
	}
	return UnfocusedColor
}

// InnerSize converts an outer width/height footprint into inner usable content space.
func InnerSize(outerWidth, outerHeight int) (width, height int) {
	width = outerWidth - 2
	if width < minWidth {
		width = minWidth
	}
	height = outerHeight - 2
	if height < minHeight {
		height = minHeight
	}
	return width, height
}

// ScrollWindow returns the [start, end) index bounds for visible rows.
func ScrollWindow(cursor, totalItems, visibleRows int) (start, end int) {
	if visibleRows < 1 {
		visibleRows = 1
	}
	if totalItems <= visibleRows {
		return 0, totalItems
	}
	maxStart := totalItems - visibleRows
	start = cursor - visibleRows + 1
	if start < 0 {
		start = 0
	}
	if start > maxStart {
		start = maxStart
	}
	end = start + visibleRows
	return start, end
}

// Frame wraps body in a rounded border with an embedded top-border title.
// e.g. "╭─ 1 Printers ──────╮"
func Frame(title, body string, width, height int, focused bool, footer ...string) string {
	lineStyle := lipgloss.NewStyle().Foreground(borderColor(focused))
	top := topBorder(title, width, focused, lineStyle)
	bottom := bottomBorder(firstFooter(footer), width, lineStyle)

	content := lipgloss.NewStyle().Width(width).Height(height).Render(body)

	var b strings.Builder
	b.WriteString(top)
	b.WriteString("\n")
	side := lineStyle.Render("│")
	lines := strings.Split(content, "\n")
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		// Pad line to exact width if needed
		lineWidth := lipgloss.Width(line)
		if lineWidth < width {
			line = line + strings.Repeat(" ", width-lineWidth)
		}
		b.WriteString(side)
		b.WriteString(line)
		b.WriteString(side)
		b.WriteString("\n")
	}
	b.WriteString(bottom)
	return b.String()
}

func firstFooter(footer []string) string {
	if len(footer) == 0 {
		return ""
	}
	return footer[0]
}

func titleLabel(title string) string {
	if len(title) > 2 && title[1] == ' ' && title[0] >= '0' && title[0] <= '9' {
		return "[" + string(title[0]) + "] " + title[2:]
	}
	return title
}

func topBorder(title string, width int, focused bool, lineStyle lipgloss.Style) string {
	label := titleLabel(title)
	if lipgloss.Width(label) > width {
		label = label[:width]
	}

	const leadDashes = 2
	fill := width - leadDashes - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}

	var b strings.Builder
	b.WriteString(lineStyle.Render("╭" + strings.Repeat("─", leadDashes)))
	b.WriteString(TitleStyle(focused).Render(label))
	b.WriteString(lineStyle.Render(strings.Repeat("─", fill) + "╮"))
	return b.String()
}

func bottomBorder(footer string, width int, lineStyle lipgloss.Style) string {
	if footer == "" || lipgloss.Width(footer) > width {
		return lineStyle.Render("╰" + strings.Repeat("─", width) + "╯")
	}

	cleanFooter := " " + strings.TrimSpace(footer) + " "
	const trailDashes = 1
	fill := width - trailDashes - lipgloss.Width(cleanFooter)
	if fill < 0 {
		fill = 0
	}

	var b strings.Builder
	b.WriteString(lineStyle.Render("╰" + strings.Repeat("─", fill)))
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(cleanFooter))
	b.WriteString(lineStyle.Render(strings.Repeat("─", trailDashes) + "╯"))
	return b.String()
}
