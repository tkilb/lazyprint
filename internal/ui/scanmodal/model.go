// Package scanmodal provides an interactive scan configuration modal.
package scanmodal

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tkilb/lazyprint/internal/config"
	"github.com/tkilb/lazyprint/internal/sane"
)

type fieldID int

const (
	fieldSource fieldID = iota
	fieldResolution
	fieldMode
	fieldFormat
	fieldAutoCrop
	fieldPath
)

const maxFields = 6

// Model represents the interactive scan modal form.
type Model struct {
	Device       sane.Device
	Capabilities sane.Capabilities
	Config       config.Config

	Sources       []string
	SourceIdx     int
	Resolutions   []int
	ResolutionIdx int
	Modes         []string
	ModeIdx       int
	Formats       []string
	FormatIdx     int
	AutoCrop      bool

	PathInput textinput.Model
	Active    fieldID
	Width     int
}

// New creates a new ScanModal model with remembered preferences or defaults.
func New(dev sane.Device, caps sane.Capabilities, cfg config.Config, prefRes int, prefFormat, prefSource, prefMode string, prefAutoCrop bool, now time.Time) Model {
	sources := caps.Sources
	if len(sources) == 0 {
		sources = []string{"Flatbed"}
	}

	resolutions := caps.Resolutions
	if len(resolutions) == 0 {
		resolutions = []int{150, 300, 600}
	}

	formats := []string{"pdf", "png", "jpeg"}

	// Find source index
	srcIdx := 0
	targetSource := prefSource
	if targetSource == "" {
		targetSource = cfg.Scan.DefaultSource
	}
	for i, s := range sources {
		if strings.EqualFold(s, targetSource) {
			srcIdx = i
			break
		}
	}

	// Find default resolution index
	resIdx := 0
	targetRes := prefRes
	if targetRes == 0 {
		targetRes = cfg.Scan.DefaultResolution
	}
	if targetRes == 0 && caps.DefaultResolution > 0 {
		targetRes = caps.DefaultResolution
	}
	for i, r := range resolutions {
		if r == targetRes {
			resIdx = i
			break
		}
	}

	// Find default mode index
	modes := caps.Modes
	if len(modes) == 0 {
		modes = []string{"Color", "Gray", "Lineart"}
	}
	modeIdx := 0
	targetMode := prefMode
	if targetMode == "" {
		targetMode = cfg.Scan.DefaultMode
	}
	if targetMode == "" {
		targetMode = "Color"
	}
	for i, md := range modes {
		if strings.EqualFold(md, targetMode) {
			modeIdx = i
			break
		}
	}

	// Find default format index
	fmtIdx := 0
	targetFormat := config.NormalizeFormat(prefFormat)
	if targetFormat == "" {
		targetFormat = config.NormalizeFormat(cfg.Scan.DefaultFormat)
	}
	if targetFormat == "" {
		targetFormat = config.DefaultScanFormat
	}
	for i, f := range formats {
		if strings.EqualFold(f, targetFormat) {
			fmtIdx = i
			break
		}
	}

	// Default path
	defaultPath := sane.BuildDefaultOutputPath(cfg.Scan.SaveDir, formats[fmtIdx], now)

	ti := textinput.New()
	ti.SetValue(defaultPath)
	ti.CharLimit = 256
	ti.Width = 40

	return Model{
		Device:        dev,
		Capabilities:  caps,
		Config:        cfg,
		Sources:       sources,
		SourceIdx:     srcIdx,
		Resolutions:   resolutions,
		ResolutionIdx: resIdx,
		Modes:         modes,
		ModeIdx:       modeIdx,
		Formats:       formats,
		FormatIdx:     fmtIdx,
		AutoCrop:      prefAutoCrop,
		PathInput:     ti,
		Active:        fieldSource,
		Width:         60,
	}
}

// Request builds the finalized sane.ScanRequest.
func (m Model) Request() sane.ScanRequest {
	source := ""
	if len(m.Sources) > 0 {
		source = m.Sources[m.SourceIdx]
	}
	res := 300
	if len(m.Resolutions) > 0 {
		res = m.Resolutions[m.ResolutionIdx]
	}
	mode := "Color"
	if len(m.Modes) > 0 {
		mode = m.Modes[m.ModeIdx]
	}
	format := config.DefaultScanFormat
	if len(m.Formats) > 0 {
		format = m.Formats[m.FormatIdx]
	}

	outPath := strings.TrimSpace(m.PathInput.Value())
	if outPath == "" {
		outPath = sane.BuildDefaultOutputPath(m.Config.Scan.SaveDir, format, time.Now())
	}

	return sane.ScanRequest{
		Device:     m.Device.Device,
		Resolution: res,
		Source:     source,
		Mode:       mode,
		Format:     format,
		OutputPath: outPath,
		AutoCrop:   m.AutoCrop,
	}
}

// Update handles modal keyboard navigation.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, nil, false

		case "tab", "down":
			m.Active = (m.Active + 1) % maxFields
			if m.Active == fieldPath {
				m.PathInput.Focus()
			} else {
				m.PathInput.Blur()
			}
			return m, nil, true

		case "shift+tab", "up":
			m.Active = (m.Active - 1 + maxFields) % maxFields
			if m.Active == fieldPath {
				m.PathInput.Focus()
			} else {
				m.PathInput.Blur()
			}
			return m, nil, true

		case "h", "left":
			if m.Active != fieldPath {
				m.cycleLeft()
				return m, nil, true
			}

		case "l", "right":
			if m.Active != fieldPath {
				m.cycleRight()
				return m, nil, true
			}

		case " ":
			if m.Active == fieldAutoCrop {
				m.AutoCrop = !m.AutoCrop
				return m, nil, true
			}
		}

		if m.Active == fieldPath {
			var cmd tea.Cmd
			m.PathInput, cmd = m.PathInput.Update(msg)
			return m, cmd, true
		}
	}

	return m, nil, true
}

func (m *Model) cycleLeft() {
	switch m.Active {
	case fieldSource:
		if len(m.Sources) > 1 {
			m.SourceIdx = (m.SourceIdx - 1 + len(m.Sources)) % len(m.Sources)
		}
	case fieldResolution:
		if len(m.Resolutions) > 1 {
			m.ResolutionIdx = (m.ResolutionIdx - 1 + len(m.Resolutions)) % len(m.Resolutions)
		}
	case fieldMode:
		if len(m.Modes) > 1 {
			m.ModeIdx = (m.ModeIdx - 1 + len(m.Modes)) % len(m.Modes)
		}
	case fieldFormat:
		if len(m.Formats) > 1 {
			m.FormatIdx = (m.FormatIdx - 1 + len(m.Formats)) % len(m.Formats)
			m.syncPathExtension()
		}
	case fieldAutoCrop:
		m.AutoCrop = !m.AutoCrop
	}
}

func (m *Model) cycleRight() {
	switch m.Active {
	case fieldSource:
		if len(m.Sources) > 1 {
			m.SourceIdx = (m.SourceIdx + 1) % len(m.Sources)
		}
	case fieldResolution:
		if len(m.Resolutions) > 1 {
			m.ResolutionIdx = (m.ResolutionIdx + 1) % len(m.Resolutions)
		}
	case fieldMode:
		if len(m.Modes) > 1 {
			m.ModeIdx = (m.ModeIdx + 1) % len(m.Modes)
		}
	case fieldFormat:
		if len(m.Formats) > 1 {
			m.FormatIdx = (m.FormatIdx + 1) % len(m.Formats)
			m.syncPathExtension()
		}
	case fieldAutoCrop:
		m.AutoCrop = !m.AutoCrop
	}
}

func (m *Model) syncPathExtension() {
	curr := m.PathInput.Value()
	if curr == "" {
		return
	}
	ext := filepath.Ext(curr)
	newExt := "." + m.Formats[m.FormatIdx]
	if ext != "" {
		m.PathInput.SetValue(curr[:len(curr)-len(ext)] + newExt)
	}
}

// View renders the scan modal dialog.
func (m Model) View() string {
	boxWidth := 56
	if m.Width > 0 && m.Width-6 < boxWidth {
		boxWidth = max(34, m.Width-6)
	}

	accent := lipgloss.Color("2")
	focusedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	formatChoice := func(val string, active bool) string {
		str := fmt.Sprintf("‹ %s ›", val)
		if active {
			return focusedStyle.Render(str)
		}
		return str
	}

	titleText := m.Device.Model
	if titleText == "" {
		titleText = m.Device.Device
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("Scan: " + titleText)

	// Source row
	sourceVal := "Flatbed"
	if len(m.Sources) > 0 {
		sourceVal = m.Sources[m.SourceIdx]
	}
	sourceStr := formatChoice(sourceVal, m.Active == fieldSource)

	// Resolution row
	resVal := "300 DPI"
	if len(m.Resolutions) > 0 {
		resVal = fmt.Sprintf("%d DPI", m.Resolutions[m.ResolutionIdx])
	}
	resStr := formatChoice(resVal, m.Active == fieldResolution)

	// Mode row
	modeVal := "Color"
	if len(m.Modes) > 0 {
		modeVal = m.Modes[m.ModeIdx]
	}
	modeStr := formatChoice(modeVal, m.Active == fieldMode)

	// Format row
	fmtVal := "PDF"
	if len(m.Formats) > 0 {
		fmtVal = strings.ToUpper(m.Formats[m.FormatIdx])
	}
	fmtStr := formatChoice(fmtVal, m.Active == fieldFormat)

	// AutoCrop row
	cropVal := "No"
	if m.AutoCrop {
		cropVal = "Yes"
	}
	cropStr := formatChoice(cropVal, m.Active == fieldAutoCrop)

	// Path row
	pathPrompt := "  "
	if m.Active == fieldPath {
		pathPrompt = "› "
	}
	pathStr := pathPrompt + m.PathInput.View()

	hint := dimStyle.Render("tab: next field  h/l: cycle  enter: scan  esc: cancel")

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		fmt.Sprintf("Source:      %s", sourceStr),
		fmt.Sprintf("Resolution:  %s", resStr),
		fmt.Sprintf("Color Mode:  %s", modeStr),
		fmt.Sprintf("Format:      %s", fmtStr),
		fmt.Sprintf("Auto-Crop:   %s", cropStr),
		"",
		"Destination file:",
		pathStr,
		"",
		hint,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 1).
		Width(boxWidth).
		Render(body)
}
