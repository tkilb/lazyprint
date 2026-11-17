// Package scanmodal provides an interactive scan configuration modal with
// para-style chooser fields and type-to-select support.
package scanmodal

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

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
	fieldAggression
	fieldPath

	maxFields
)

// Model represents the interactive scan modal form.
type Model struct {
	Device       sane.Device
	Capabilities sane.Capabilities
	Config       config.Config

	Sources          []string
	SourceIdx        int
	Resolutions      []int
	ResolutionIdx    int
	Modes            []string
	ModeIdx          int
	Formats          []string
	FormatIdx        int
	AutoCrop         bool
	Aggressions      []sane.CropAggression
	AggressionIdx    int
	lastNonOffAggIdx int

	typed     string
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

	aggressions := sane.CropAggressions()
	aggIdx := 3 // Crop30Pct ("30%")
	lastNonOff := 3
	targetAgg := sane.ParseCropAggression(cfg.Scan.CropAggression)
	for i, a := range aggressions {
		if a == targetAgg {
			aggIdx = i
			if a != sane.CropOff {
				lastNonOff = i
			}
			break
		}
	}
	if !prefAutoCrop {
		aggIdx = 0
	} else if aggIdx == 0 {
		aggIdx = lastNonOff
	}

	// Default path
	defaultPath := sane.BuildDefaultOutputPath(cfg.Scan.SaveDir, formats[fmtIdx], now)

	ti := textinput.New()
	ti.SetValue(defaultPath)
	ti.CharLimit = 256
	ti.Width = 44

	return Model{
		Device:           dev,
		Capabilities:     caps,
		Config:           cfg,
		Sources:          sources,
		SourceIdx:        srcIdx,
		Resolutions:      resolutions,
		ResolutionIdx:    resIdx,
		Modes:            modes,
		ModeIdx:          modeIdx,
		Formats:          formats,
		FormatIdx:        fmtIdx,
		AutoCrop:         prefAutoCrop,
		Aggressions:      aggressions,
		AggressionIdx:    aggIdx,
		lastNonOffAggIdx: lastNonOff,
		PathInput:        ti,
		Active:           fieldSource,
		Width:            68,
	}
}

// WithAggression sets the preferred crop aggression preset on the modal, keeping AutoCrop synced.
func (m Model) WithAggression(prefAggression string) Model {
	if strings.TrimSpace(prefAggression) == "" {
		return m
	}
	target := sane.ParseCropAggression(prefAggression)
	for i, a := range m.Aggressions {
		if a == target {
			if a != sane.CropOff {
				m.lastNonOffAggIdx = i
			}
			if m.AutoCrop {
				if a == sane.CropOff {
					m.AggressionIdx = m.lastNonOffAggIdx
				} else {
					m.AggressionIdx = i
				}
			} else {
				m.AggressionIdx = 0
			}
			break
		}
	}
	return m
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

	agg := sane.CropDefault
	if len(m.Aggressions) > 0 && m.AggressionIdx >= 0 && m.AggressionIdx < len(m.Aggressions) {
		agg = m.Aggressions[m.AggressionIdx]
	}
	autoCrop := m.AutoCrop && agg != sane.CropOff

	outPath := strings.TrimSpace(m.PathInput.Value())
	if outPath == "" {
		outPath = sane.BuildDefaultOutputPath(m.Config.Scan.SaveDir, format, time.Now())
	}

	return sane.ScanRequest{
		Device:         m.Device.Device,
		Resolution:     res,
		Source:         source,
		Mode:           mode,
		Format:         format,
		OutputPath:     outPath,
		AutoCrop:       autoCrop,
		CropAggression: agg.String(),
	}
}

// Update handles modal keyboard navigation, left/right cycling, and para-style type-to-select.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, nil, false

		case "tab", "down":
			m.Active = (m.Active + 1) % maxFields
			m.typed = ""
			if m.Active == fieldPath {
				m.PathInput.Focus()
			} else {
				m.PathInput.Blur()
			}
			return m, nil, true

		case "shift+tab", "up":
			m.Active = (m.Active - 1 + maxFields) % maxFields
			m.typed = ""
			if m.Active == fieldPath {
				m.PathInput.Focus()
			} else {
				m.PathInput.Blur()
			}
			return m, nil, true

		case "h", "left":
			if m.Active != fieldPath {
				m.typed = ""
				m.cycleLeft()
				return m, nil, true
			}

		case "l", "right":
			if m.Active != fieldPath {
				m.typed = ""
				m.cycleRight()
				return m, nil, true
			}

		case " ":
			if m.Active == fieldAutoCrop {
				m.typed = ""
				m.setAutoCrop(!m.AutoCrop)
				return m, nil, true
			} else if m.Active == fieldAggression {
				m.typed = ""
				m.cycleRight()
				return m, nil, true
			}
		}

		if m.Active == fieldPath {
			var cmd tea.Cmd
			m.PathInput, cmd = m.PathInput.Update(msg)
			return m, cmd, true
		}

		m.handleChooserTypeSelect(msg)
		return m, nil, true
	}

	return m, nil, true
}

func (m *Model) setAutoCrop(enabled bool) {
	m.AutoCrop = enabled
	if !enabled {
		m.AggressionIdx = 0
	} else if m.AggressionIdx == 0 {
		if m.lastNonOffAggIdx > 0 && m.lastNonOffAggIdx < len(m.Aggressions) {
			m.AggressionIdx = m.lastNonOffAggIdx
		} else {
			m.AggressionIdx = 3
		}
	}
}

func (m *Model) syncFromAggression() {
	if len(m.Aggressions) == 0 {
		return
	}
	agg := m.Aggressions[m.AggressionIdx]
	if agg == sane.CropOff {
		m.AutoCrop = false
	} else {
		m.AutoCrop = true
		m.lastNonOffAggIdx = m.AggressionIdx
	}
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
		m.setAutoCrop(!m.AutoCrop)
	case fieldAggression:
		if len(m.Aggressions) > 1 {
			m.AggressionIdx = (m.AggressionIdx - 1 + len(m.Aggressions)) % len(m.Aggressions)
			m.syncFromAggression()
		}
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
		m.setAutoCrop(!m.AutoCrop)
	case fieldAggression:
		if len(m.Aggressions) > 1 {
			m.AggressionIdx = (m.AggressionIdx + 1) % len(m.Aggressions)
			m.syncFromAggression()
		}
	}
}

// handleChooserTypeSelect implements para's typeSelect behavior on chooser fields:
// typing letters jumps to the matching option prefix, and Backspace clears/resets.
func (m *Model) handleChooserTypeSelect(key tea.KeyMsg) {
	switch m.Active {
	case fieldSource:
		m.SourceIdx, m.typed = typeSelect(key, m.Sources, m.SourceIdx, m.typed, 0)
	case fieldResolution:
		labels := make([]string, len(m.Resolutions))
		for i, r := range m.Resolutions {
			labels[i] = fmt.Sprintf("%d", r)
		}
		m.ResolutionIdx, m.typed = typeSelect(key, labels, m.ResolutionIdx, m.typed, 0)
	case fieldMode:
		m.ModeIdx, m.typed = typeSelect(key, m.Modes, m.ModeIdx, m.typed, 0)
	case fieldFormat:
		prev := m.FormatIdx
		m.FormatIdx, m.typed = typeSelect(key, m.Formats, m.FormatIdx, m.typed, 0)
		if m.FormatIdx != prev {
			m.syncPathExtension()
		}
	case fieldAutoCrop:
		labels := []string{"No", "Yes"}
		idx := 0
		if m.AutoCrop {
			idx = 1
		}
		nextIdx, nextTyped := typeSelect(key, labels, idx, m.typed, 0)
		m.typed = nextTyped
		if nextIdx != idx {
			m.setAutoCrop(nextIdx == 1)
		}
	case fieldAggression:
		labels := make([]string, len(m.Aggressions))
		for i, a := range m.Aggressions {
			labels[i] = a.String()
		}
		// Like para's Kind chooser, Backspace clears to index 0 (off) and typing searches presets from index 1 first
		from := 1
		if key.Type == tea.KeyRunes {
			next := strings.ToLower(m.typed + string(key.Runes))
			if strings.HasPrefix("off", next) {
				from = 0
			} else if next == "0" {
				m.AggressionIdx = 0
				m.typed = "0"
				m.syncFromAggression()
				return
			}
		}
		prev := m.AggressionIdx
		m.AggressionIdx, m.typed = typeSelect(key, labels, m.AggressionIdx, m.typed, from)
		if m.AggressionIdx != prev {
			m.syncFromAggression()
		}
	}
}

func typeSelect(key tea.KeyMsg, labels []string, idx int, typed string, from int) (int, string) {
	switch {
	case key.Type == tea.KeyBackspace:
		if typed != "" {
			_, size := utf8.DecodeLastRuneInString(typed)
			typed = typed[:len(typed)-size]
		}
		if typed == "" {
			return 0, ""
		}
		return matchLabel(labels, typed, idx, from), typed
	case key.Type == tea.KeyRunes && !key.Alt:
		next := typed + strings.ToLower(string(key.Runes))
		if i := matchLabel(labels, next, -1, from); i >= 0 {
			return i, next
		}
		return idx, typed
	}
	return idx, typed
}

func matchLabel(labels []string, prefix string, fallback, from int) int {
	for i := from; i < len(labels); i++ {
		if strings.HasPrefix(strings.ToLower(labels[i]), prefix) {
			return i
		}
	}
	return fallback
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

// View renders the scan modal dialog with para-style chooser lines.
func (m Model) View() string {
	boxWidth := 68
	if m.Width > 0 && m.Width-6 < boxWidth {
		boxWidth = max(38, m.Width-6)
	}

	accent := lipgloss.Color("2")
	focusedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	formatChoice := func(val, meaning string, active bool) string {
		var renderedVal string
		if active {
			renderedVal = focusedStyle.Render(" " + val + " ")
		} else {
			renderedVal = lipgloss.NewStyle().Bold(true).Render(val)
		}
		suffix := ""
		if meaning != "" {
			suffix += "  " + meaning
		}
		if active && m.typed != "" {
			suffix += "  [" + m.typed + "]"
		}
		return dimStyle.Render("‹ ") + renderedVal + dimStyle.Render(" ›"+suffix)
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
	sourceStr := formatChoice(sourceVal, "", m.Active == fieldSource)

	// Resolution row
	resVal := "300 DPI"
	if len(m.Resolutions) > 0 {
		resVal = fmt.Sprintf("%d DPI", m.Resolutions[m.ResolutionIdx])
	}
	resStr := formatChoice(resVal, "", m.Active == fieldResolution)

	// Mode row
	modeVal := "Color"
	if len(m.Modes) > 0 {
		modeVal = m.Modes[m.ModeIdx]
	}
	modeStr := formatChoice(modeVal, "", m.Active == fieldMode)

	// Format row
	fmtVal := "PDF"
	if len(m.Formats) > 0 {
		fmtVal = strings.ToUpper(m.Formats[m.FormatIdx])
	}
	fmtStr := formatChoice(fmtVal, "", m.Active == fieldFormat)

	// AutoCrop row
	cropVal := "No"
	cropMeaning := "Keep full scanner bed"
	if m.AutoCrop {
		cropVal = "Yes"
		cropMeaning = "Trim empty scanner bed"
	}
	cropStr := formatChoice(cropVal, cropMeaning, m.Active == fieldAutoCrop)

	// Aggression row (para-style chooser with meaning + typeSelect, no foreground color)
	agg := sane.CropDefault
	if len(m.Aggressions) > 0 && m.AggressionIdx >= 0 && m.AggressionIdx < len(m.Aggressions) {
		agg = m.Aggressions[m.AggressionIdx]
	}
	aggStr := formatChoice(agg.String(), agg.Meaning(), m.Active == fieldAggression)

	// Path row
	pathPrompt := "  "
	if m.Active == fieldPath {
		pathPrompt = "› "
	}
	pathStr := pathPrompt + m.PathInput.View()

	hint := dimStyle.Render("(type) jump  (←/→) change  (⌫) reset  (tab) move  (enter) scan")

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		fmt.Sprintf("Source:      %s", sourceStr),
		fmt.Sprintf("Resolution:  %s", resStr),
		fmt.Sprintf("Color Mode:  %s", modeStr),
		fmt.Sprintf("Format:      %s", fmtStr),
		fmt.Sprintf("Auto-Crop:   %s", cropStr),
		fmt.Sprintf("Aggression:  %s", aggStr),
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
