// Package app contains the top-level Bubble Tea model and layout orchestration.
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tkilb/lazyprint/internal/config"
	"github.com/tkilb/lazyprint/internal/cups"
	"github.com/tkilb/lazyprint/internal/runner"
	"github.com/tkilb/lazyprint/internal/sane"
	"github.com/tkilb/lazyprint/internal/ui/panel"
	"github.com/tkilb/lazyprint/internal/ui/popup"
	"github.com/tkilb/lazyprint/internal/ui/statusbar"
)

type actionType int

const (
	actionNone actionType = iota
	actionDeletePrinter
	actionCancelJob
	actionPurgeJobs
)

type actionCompletedMsg struct {
	message string
	err     error
	refresh bool
}

type focusCycle []statusbar.PanelID

var defaultCycle = focusCycle{
	statusbar.PanelPrinters,
	statusbar.PanelJobs,
	statusbar.PanelScanners,
	statusbar.PanelDetails,
}

// Data messages.
type printersMsg struct {
	printers []cups.Printer
	err      error
}

type jobsMsg struct {
	jobs []cups.Job
	err  error
}

type scannersMsg struct {
	devices []sane.Device
	err     error
}

type printerOptionsMsg struct {
	printer string
	options []cups.PrinterOption
}

type scannerCapsMsg struct {
	device string
	caps   sane.Capabilities
}

// Model is the main application model.
type Model struct {
	Config config.Config
	Runner runner.Runner
	CUPS   *cups.Client
	SANE   *sane.Client

	Width  int
	Height int

	Focus          statusbar.PanelID
	StatusBar      statusbar.Model
	PrinterCursor  int
	JobCursor      int
	ScannerCursor  int
	DetailsScroll  int

	Printers       []cups.Printer
	Jobs           []cups.Job
	Scanners       []sane.Device
	PrinterOptions map[string][]cups.PrinterOption
	ScannerCaps    map[string]sane.Capabilities

	LoadingPrinters bool
	LoadingJobs     bool
	LoadingScanners bool

	// Popups and actions
	Popups        popup.Model
	ConfirmAction actionType
	ConfirmText   string
	DangerConfirm bool

	// Rename prompt modal
	Renaming     bool
	RenameTarget cups.Printer
	RenameInput  textinput.Model
}

// NewModel creates an initialized Model with the provided configuration and runner.
func NewModel(cfg config.Config, r runner.Runner) Model {
	if r == nil {
		r = runner.NewRealRunner()
	}
	cupsClient := cups.NewClient(r)
	saneClient := sane.NewClient(r)

	ti := textinput.New()
	ti.Placeholder = "new-printer-name"
	ti.CharLimit = 64

	return Model{
		Config:          cfg,
		Runner:          r,
		CUPS:            cupsClient,
		SANE:            saneClient,
		Width:           100,
		Height:          30,
		Focus:           statusbar.PanelPrinters,
		StatusBar:       statusbar.New(),
		PrinterOptions:  make(map[string][]cups.PrinterOption),
		ScannerCaps:     make(map[string]sane.Capabilities),
		LoadingPrinters: true,
		LoadingJobs:     true,
		LoadingScanners: true,
		RenameInput:     ti,
	}
}

// Init triggers initial asynchronous data loads.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchPrintersCmd(),
		m.fetchJobsCmd(),
		m.fetchScannersCmd(),
	)
}

func (m Model) fetchPrintersCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p, err := m.CUPS.GetPrinters(ctx)
		return printersMsg{printers: p, err: err}
	}
}

func (m Model) fetchJobsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		j, err := m.CUPS.GetJobs(ctx)
		return jobsMsg{jobs: j, err: err}
	}
}

func (m Model) fetchScannersCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		s, err := m.SANE.GetDevices(ctx)
		return scannersMsg{devices: s, err: err}
	}
}

func (m Model) fetchPrinterOptionsCmd(printer string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		opts, err := m.CUPS.GetOptions(ctx, printer)
		if err != nil {
			return nil
		}
		return printerOptionsMsg{printer: printer, options: opts}
	}
}

func (m Model) fetchScannerCapsCmd(device string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		caps, err := m.SANE.GetCapabilities(ctx, device)
		if err != nil {
			return nil
		}
		return scannerCapsMsg{device: device, caps: caps}
	}
}

// Update handles Bubble Tea messages and interactions.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.StatusBar.Width = msg.Width

	case printersMsg:
		m.LoadingPrinters = false
		if msg.err != nil {
			m.StatusBar.SetMessage(fmt.Sprintf("CUPS error: %v", msg.err), true)
		} else {
			m.Printers = msg.printers
			if m.PrinterCursor >= len(m.Printers) {
				m.PrinterCursor = max(0, len(m.Printers)-1)
			}
			if len(m.Printers) > 0 {
				selected := m.Printers[m.PrinterCursor].Name
				if _, ok := m.PrinterOptions[selected]; !ok {
					cmds = append(cmds, m.fetchPrinterOptionsCmd(selected))
				}
			}
		}

	case jobsMsg:
		m.LoadingJobs = false
		if msg.err != nil {
			m.StatusBar.SetMessage(fmt.Sprintf("Queue error: %v", msg.err), true)
		} else {
			m.Jobs = msg.jobs
			if m.JobCursor >= len(m.Jobs) {
				m.JobCursor = max(0, len(m.Jobs)-1)
			}
		}

	case scannersMsg:
		m.LoadingScanners = false
		if msg.err != nil {
			m.StatusBar.SetMessage(fmt.Sprintf("SANE error: %v", msg.err), true)
		} else {
			m.Scanners = msg.devices
			if m.ScannerCursor >= len(m.Scanners) {
				m.ScannerCursor = max(0, len(m.Scanners)-1)
			}
			if len(m.Scanners) > 0 {
				selected := m.Scanners[m.ScannerCursor].Device
				if _, ok := m.ScannerCaps[selected]; !ok {
					cmds = append(cmds, m.fetchScannerCapsCmd(selected))
				}
			}
		}

	case printerOptionsMsg:
		if msg.options != nil {
			m.PrinterOptions[msg.printer] = msg.options
		}

	case scannerCapsMsg:
		m.ScannerCaps[msg.device] = msg.caps

	case actionCompletedMsg:
		if msg.err != nil {
			m.Popups = m.Popups.Push(popup.Message{Severity: popup.Error, Text: msg.err.Error()})
		} else if msg.message != "" {
			m.StatusBar.SetMessage(msg.message, false)
		}
		if msg.refresh {
			cmds = append(cmds, m.fetchPrintersCmd(), m.fetchJobsCmd())
		}

	case tea.KeyMsg:
		if m.Popups.Active() {
			m.Popups = m.Popups.Dismiss()
			return m, nil
		}

		if m.ConfirmAction != actionNone {
			switch msg.String() {
			case "y", "enter":
				actionCmd := m.executeConfirmAction()
				m.ConfirmAction = actionNone
				m.ConfirmText = ""
				m.DangerConfirm = false
				return m, actionCmd
			case "n", "esc", "q":
				m.ConfirmAction = actionNone
				m.ConfirmText = ""
				m.DangerConfirm = false
				return m, nil
			}
			return m, nil
		}

		if m.Renaming {
			switch msg.String() {
			case "esc":
				m.Renaming = false
				m.RenameInput.Blur()
				return m, nil
			case "enter":
				renameCmd := m.confirmRename()
				m.Renaming = false
				m.RenameInput.Blur()
				return m, renameCmd
			}
			var tiCmd tea.Cmd
			m.RenameInput, tiCmd = m.RenameInput.Update(msg)
			return m, tiCmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "0":
			m.Focus = statusbar.PanelDetails
		case "1":
			m.Focus = statusbar.PanelPrinters
		case "2":
			m.Focus = statusbar.PanelJobs
		case "3":
			m.Focus = statusbar.PanelScanners

		case "tab":
			m.Focus = m.nextFocus()
		case "shift+tab":
			m.Focus = m.prevFocus()

		case "r":
			m.LoadingPrinters = true
			m.LoadingJobs = true
			m.LoadingScanners = true
			m.StatusBar.SetMessage("Refreshing devices...", false)
			cmds = append(cmds, m.fetchPrintersCmd(), m.fetchJobsCmd(), m.fetchScannersCmd())

		case "j", "down":
			m.moveCursorDown(&cmds)
		case "k", "up":
			m.moveCursorUp(&cmds)

		case "d":
			if m.Focus == statusbar.PanelPrinters && len(m.Printers) > 0 {
				cmds = append(cmds, m.setDefaultPrinterCmd())
			}
		case " ":
			if m.Focus == statusbar.PanelPrinters && len(m.Printers) > 0 {
				cmds = append(cmds, m.togglePausePrinterCmd())
			}
		case "x":
			if m.Focus == statusbar.PanelPrinters && len(m.Printers) > 0 {
				target := m.Printers[m.PrinterCursor]
				m.ConfirmAction = actionDeletePrinter
				m.ConfirmText = fmt.Sprintf("Delete printer queue %q?", target.Name)
				m.DangerConfirm = true
			} else if m.Focus == statusbar.PanelJobs && len(m.Jobs) > 0 {
				target := m.Jobs[m.JobCursor]
				m.ConfirmAction = actionCancelJob
				m.ConfirmText = fmt.Sprintf("Cancel print job %s?", target.ID)
				m.DangerConfirm = false
			}
		case "X":
			if m.Focus == statusbar.PanelJobs && len(m.Jobs) > 0 {
				m.ConfirmAction = actionPurgeJobs
				m.ConfirmText = "Purge all active print jobs in the queue?"
				m.DangerConfirm = true
			}
		case "R", "e":
			if m.Focus == statusbar.PanelPrinters && len(m.Printers) > 0 {
				target := m.Printers[m.PrinterCursor]
				m.Renaming = true
				m.RenameTarget = target
				m.RenameInput.SetValue(target.Name)
				m.RenameInput.Focus()
			}
		}

	case tea.MouseMsg:
		if m.Config.UI.Mouse {
			cmd := m.handleMouse(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) nextFocus() statusbar.PanelID {
	for i, f := range defaultCycle {
		if f == m.Focus {
			return defaultCycle[(i+1)%len(defaultCycle)]
		}
	}
	return statusbar.PanelPrinters
}

func (m *Model) prevFocus() statusbar.PanelID {
	for i, f := range defaultCycle {
		if f == m.Focus {
			return defaultCycle[(i-1+len(defaultCycle))%len(defaultCycle)]
		}
	}
	return statusbar.PanelPrinters
}

func (m *Model) moveCursorDown(cmds *[]tea.Cmd) {
	switch m.Focus {
	case statusbar.PanelPrinters:
		if m.PrinterCursor < len(m.Printers)-1 {
			m.PrinterCursor++
			selected := m.Printers[m.PrinterCursor].Name
			if _, ok := m.PrinterOptions[selected]; !ok {
				*cmds = append(*cmds, m.fetchPrinterOptionsCmd(selected))
			}
		}
	case statusbar.PanelJobs:
		if m.JobCursor < len(m.Jobs)-1 {
			m.JobCursor++
		}
	case statusbar.PanelScanners:
		if m.ScannerCursor < len(m.Scanners)-1 {
			m.ScannerCursor++
			selected := m.Scanners[m.ScannerCursor].Device
			if _, ok := m.ScannerCaps[selected]; !ok {
				*cmds = append(*cmds, m.fetchScannerCapsCmd(selected))
			}
		}
	case statusbar.PanelDetails:
		m.DetailsScroll++
	}
}

func (m *Model) moveCursorUp(cmds *[]tea.Cmd) {
	switch m.Focus {
	case statusbar.PanelPrinters:
		if m.PrinterCursor > 0 {
			m.PrinterCursor--
			selected := m.Printers[m.PrinterCursor].Name
			if _, ok := m.PrinterOptions[selected]; !ok {
				*cmds = append(*cmds, m.fetchPrinterOptionsCmd(selected))
			}
		}
	case statusbar.PanelJobs:
		if m.JobCursor > 0 {
			m.JobCursor--
		}
	case statusbar.PanelScanners:
		if m.ScannerCursor > 0 {
			m.ScannerCursor--
			selected := m.Scanners[m.ScannerCursor].Device
			if _, ok := m.ScannerCaps[selected]; !ok {
				*cmds = append(*cmds, m.fetchScannerCapsCmd(selected))
			}
		}
	case statusbar.PanelDetails:
		if m.DetailsScroll > 0 {
			m.DetailsScroll--
		}
	}
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress {
		return nil
	}

	leftWidth, p1H, p2H, p3H := m.calculateLayout()
	y := msg.Y
	x := msg.X

	// Wheel scrolling
	if msg.Button == tea.MouseButtonWheelUp {
		var cmds []tea.Cmd
		m.moveCursorUp(&cmds)
		return tea.Batch(cmds...)
	} else if msg.Button == tea.MouseButtonWheelDown {
		var cmds []tea.Cmd
		m.moveCursorDown(&cmds)
		return tea.Batch(cmds...)
	}

	if msg.Button != tea.MouseButtonLeft {
		return nil
	}

	if x < leftWidth {
		// Click in left column
		if y < p1H {
			m.Focus = statusbar.PanelPrinters
			clickedRow := y - 1 // 1 top border
			if clickedRow >= 0 && clickedRow < len(m.Printers) {
				m.PrinterCursor = clickedRow
				selected := m.Printers[m.PrinterCursor].Name
				if _, ok := m.PrinterOptions[selected]; !ok {
					return m.fetchPrinterOptionsCmd(selected)
				}
			}
		} else if y < p1H+p2H {
			m.Focus = statusbar.PanelJobs
			clickedRow := y - p1H - 1
			if clickedRow >= 0 && clickedRow < len(m.Jobs) {
				m.JobCursor = clickedRow
			}
		} else if y < p1H+p2H+p3H {
			m.Focus = statusbar.PanelScanners
			clickedRow := y - p1H - p2H - 1
			if clickedRow >= 0 && clickedRow < len(m.Scanners) {
				m.ScannerCursor = clickedRow
				selected := m.Scanners[m.ScannerCursor].Device
				if _, ok := m.ScannerCaps[selected]; !ok {
					return m.fetchScannerCapsCmd(selected)
				}
			}
		}
	} else {
		// Click in right column (Details)
		m.Focus = statusbar.PanelDetails
	}

	return nil
}

func (m *Model) setDefaultPrinterCmd() tea.Cmd {
	printer := m.Printers[m.PrinterCursor].Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := m.CUPS.SetDefault(ctx, printer)
		if err != nil {
			return actionCompletedMsg{err: err}
		}
		return actionCompletedMsg{message: fmt.Sprintf("Default printer set to %s", printer), refresh: true}
	}
}

func (m *Model) togglePausePrinterCmd() tea.Cmd {
	target := m.Printers[m.PrinterCursor]
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if target.Status == cups.StatusDisabled || !target.Enabled {
			err := m.CUPS.ResumePrinter(ctx, target.Name)
			if err != nil {
				return actionCompletedMsg{err: err}
			}
			return actionCompletedMsg{message: fmt.Sprintf("Resumed printer %s", target.Name), refresh: true}
		} else {
			err := m.CUPS.PausePrinter(ctx, target.Name)
			if err != nil {
				return actionCompletedMsg{err: err}
			}
			return actionCompletedMsg{message: fmt.Sprintf("Paused printer %s", target.Name), refresh: true}
		}
	}
}

func (m *Model) executeConfirmAction() tea.Cmd {
	switch m.ConfirmAction {
	case actionDeletePrinter:
		if len(m.Printers) == 0 || m.PrinterCursor >= len(m.Printers) {
			return nil
		}
		target := m.Printers[m.PrinterCursor].Name
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := m.CUPS.DeletePrinter(ctx, target)
			if err != nil {
				return actionCompletedMsg{err: err}
			}
			return actionCompletedMsg{message: fmt.Sprintf("Deleted printer %s", target), refresh: true}
		}

	case actionCancelJob:
		if len(m.Jobs) == 0 || m.JobCursor >= len(m.Jobs) {
			return nil
		}
		target := m.Jobs[m.JobCursor].ID
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := m.CUPS.CancelJob(ctx, target)
			if err != nil {
				return actionCompletedMsg{err: err}
			}
			return actionCompletedMsg{message: fmt.Sprintf("Cancelled job %s", target), refresh: true}
		}

	case actionPurgeJobs:
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := m.CUPS.CancelAll(ctx, "")
			if err != nil {
				return actionCompletedMsg{err: err}
			}
			return actionCompletedMsg{message: "Purged all queue jobs", refresh: true}
		}
	}
	return nil
}

func (m *Model) confirmRename() tea.Cmd {
	oldName := m.RenameTarget.Name
	newName := strings.TrimSpace(m.RenameInput.Value())
	if newName == "" || newName == oldName {
		return nil
	}
	uri := m.RenameTarget.DeviceURI
	isDefault := m.RenameTarget.IsDefault

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		err := m.CUPS.RenamePrinter(ctx, oldName, newName, uri, isDefault)
		if err != nil {
			return actionCompletedMsg{err: err}
		}
		return actionCompletedMsg{message: fmt.Sprintf("Renamed %s to %s", oldName, newName), refresh: true}
	}
}

func (m Model) calculateLayout() (leftWidth, p1H, p2H, p3H int) {
	statusH := 1
	usableH := max(8, m.Height-statusH)

	leftWidth = m.Width / 2
	if leftWidth < 30 {
		leftWidth = min(m.Width, 30)
	}

	// Panel [1] Printers is sized for up to ~3 printers (outer height 5 = border + 3 rows + border)
	p1H = 5
	if usableH < 12 {
		p1H = max(3, usableH/3)
	}

	remainingH := usableH - p1H
	p2H = remainingH / 2
	p3H = remainingH - p2H
	return leftWidth, p1H, p2H, p3H
}

// View renders the 2-column grid and status bar, with active modal popups overlaid.
func (m Model) View() string {
	leftWidth, p1H, p2H, p3H := m.calculateLayout()
	rightWidth := max(20, m.Width-leftWidth)
	rightHeight := p1H + p2H + p3H

	leftCol := lipgloss.JoinVertical(lipgloss.Left,
		m.renderPrintersPanel(leftWidth, p1H),
		m.renderJobsPanel(leftWidth, p2H),
		m.renderScannersPanel(leftWidth, p3H),
	)

	rightCol := m.renderDetailsPanel(rightWidth, rightHeight)

	grid := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)
	status := m.StatusBar.View(m.Focus)

	baseView := lipgloss.JoinVertical(lipgloss.Left, grid, status)

	if m.Renaming {
		box := popup.PromptBox("Rename Printer", fmt.Sprintf("Enter new queue name for %s:", m.RenameTarget.Name), m.RenameInput.View(), m.Width)
		return popup.Overlay(baseView, box, m.Width, m.Height)
	}

	if m.ConfirmAction != actionNone {
		var box string
		if m.DangerConfirm {
			box = popup.DangerConfirmBox(m.ConfirmText, m.Width)
		} else {
			box = popup.ConfirmBox(m.ConfirmText, m.Width)
		}
		return popup.Overlay(baseView, box, m.Width, m.Height)
	}

	if m.Popups.Active() {
		msg, _ := m.Popups.Current()
		box := popup.Box(msg, m.Width)
		return popup.Overlay(baseView, box, m.Width, m.Height)
	}

	return baseView
}

func (m Model) renderPrintersPanel(width, height int) string {
	innerW, innerH := panel.InnerSize(width, height)
	var rows []string

	if m.LoadingPrinters {
		rows = append(rows, " Loading printers...")
	} else if len(m.Printers) == 0 {
		rows = append(rows, " (no printers found)")
	} else {
		start, end := panel.ScrollWindow(m.PrinterCursor, len(m.Printers), innerH)
		for i := start; i < end; i++ {
			p := m.Printers[i]
			marker := "  "
			if p.IsDefault {
				marker = "* "
			}

			statusStr := fmt.Sprintf("[%s]", p.Status)
			label := fmt.Sprintf("%s%-18s %s", marker, truncate(p.Name, 18), statusStr)

			if i == m.PrinterCursor && m.Focus == statusbar.PanelPrinters {
				rows = append(rows, panel.SelectedRowStyle(innerW).Render(truncate(label, innerW)))
			} else {
				rows = append(rows, truncate(label, innerW))
			}
		}
	}

	footer := ""
	if len(m.Printers) > 0 {
		footer = fmt.Sprintf("%d/%d", m.PrinterCursor+1, len(m.Printers))
	}

	return panel.Frame("[1] Printers", strings.Join(rows, "\n"), innerW, innerH, m.Focus == statusbar.PanelPrinters, footer)
}

func (m Model) renderJobsPanel(width, height int) string {
	innerW, innerH := panel.InnerSize(width, height)
	var rows []string

	if m.LoadingJobs {
		rows = append(rows, " Loading queue...")
	} else if len(m.Jobs) == 0 {
		rows = append(rows, " (no active jobs)")
	} else {
		start, end := panel.ScrollWindow(m.JobCursor, len(m.Jobs), innerH)
		for i := start; i < end; i++ {
			j := m.Jobs[i]
			label := fmt.Sprintf(" %-16s %-10s %dB", truncate(j.ID, 16), truncate(j.User, 10), j.Size)
			if i == m.JobCursor && m.Focus == statusbar.PanelJobs {
				rows = append(rows, panel.SelectedRowStyle(innerW).Render(truncate(label, innerW)))
			} else {
				rows = append(rows, truncate(label, innerW))
			}
		}
	}

	footer := ""
	if len(m.Jobs) > 0 {
		footer = fmt.Sprintf("%d/%d", m.JobCursor+1, len(m.Jobs))
	}

	return panel.Frame("[2] Jobs", strings.Join(rows, "\n"), innerW, innerH, m.Focus == statusbar.PanelJobs, footer)
}

func (m Model) renderScannersPanel(width, height int) string {
	innerW, innerH := panel.InnerSize(width, height)
	var rows []string

	if m.LoadingScanners {
		rows = append(rows, " Discovering scanners...")
	} else if len(m.Scanners) == 0 {
		rows = append(rows, " (no scanners detected)")
	} else {
		start, end := panel.ScrollWindow(m.ScannerCursor, len(m.Scanners), innerH)
		for i := start; i < end; i++ {
			s := m.Scanners[i]
			displayName := s.Model
			if displayName == "" {
				displayName = s.Device
			}
			vendorBadge := ""
			if s.Vendor != "" {
				vendorBadge = fmt.Sprintf("[%s]", s.Vendor)
			}
			label := fmt.Sprintf(" %-22s %s", truncate(displayName, 22), vendorBadge)

			if i == m.ScannerCursor && m.Focus == statusbar.PanelScanners {
				rows = append(rows, panel.SelectedRowStyle(innerW).Render(truncate(label, innerW)))
			} else {
				rows = append(rows, truncate(label, innerW))
			}
		}
	}

	footer := ""
	if len(m.Scanners) > 0 {
		footer = fmt.Sprintf("%d/%d", m.ScannerCursor+1, len(m.Scanners))
	}

	return panel.Frame("[3] Scanners", strings.Join(rows, "\n"), innerW, innerH, m.Focus == statusbar.PanelScanners, footer)
}

func (m Model) renderDetailsPanel(width, height int) string {
	innerW, innerH := panel.InnerSize(width, height)
	var lines []string

	hStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	switch m.Focus {
	case statusbar.PanelPrinters:
		if len(m.Printers) > 0 && m.PrinterCursor < len(m.Printers) {
			p := m.Printers[m.PrinterCursor]
			lines = append(lines, hStyle.Render("Printer Details"))
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("Name:        %s", p.Name))
			defaultStr := "No"
			if p.IsDefault {
				defaultStr = "Yes (System Default)"
			}
			lines = append(lines, fmt.Sprintf("Default:     %s", defaultStr))
			lines = append(lines, fmt.Sprintf("Status:      %s", p.Status))
			if p.Since != "" {
				lines = append(lines, fmt.Sprintf("Since:       %s", p.Since))
			}
			if p.StatusMessage != "" {
				lines = append(lines, fmt.Sprintf("Message:     %s", p.StatusMessage))
			}
			if p.DeviceURI != "" {
				lines = append(lines, fmt.Sprintf("Device URI:  %s", p.DeviceURI))
			}

			if opts, ok := m.PrinterOptions[p.Name]; ok && len(opts) > 0 {
				lines = append(lines, "")
				lines = append(lines, hStyle.Render("Configured Options (PPD)"))
				for _, opt := range opts {
					def := opt.Default
					if def == "" {
						def = "Auto"
					}
					lines = append(lines, fmt.Sprintf("  • %-16s: %s", opt.Name, def))
				}
			}
		} else {
			lines = append(lines, dimStyle.Render("No printer selected."))
		}

	case statusbar.PanelJobs:
		if len(m.Jobs) > 0 && m.JobCursor < len(m.Jobs) {
			j := m.Jobs[m.JobCursor]
			lines = append(lines, hStyle.Render("Print Job Details"))
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("Job ID:      %s", j.ID))
			lines = append(lines, fmt.Sprintf("Printer:     %s", j.Printer))
			lines = append(lines, fmt.Sprintf("Job Number:  %d", j.JobNumber))
			lines = append(lines, fmt.Sprintf("User:        %s", j.User))
			lines = append(lines, fmt.Sprintf("Size:        %d bytes", j.Size))
			lines = append(lines, fmt.Sprintf("Submitted:   %s", j.SubmittedAt))
		} else {
			lines = append(lines, dimStyle.Render("No active jobs in queue."))
		}

	case statusbar.PanelScanners:
		if len(m.Scanners) > 0 && m.ScannerCursor < len(m.Scanners) {
			s := m.Scanners[m.ScannerCursor]
			lines = append(lines, hStyle.Render("Scanner Details"))
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("Device:      %s", s.Device))
			if s.Vendor != "" {
				lines = append(lines, fmt.Sprintf("Vendor:      %s", s.Vendor))
			}
			if s.Model != "" {
				lines = append(lines, fmt.Sprintf("Model:       %s", s.Model))
			}
			if s.Type != "" {
				lines = append(lines, fmt.Sprintf("Type:        %s", s.Type))
			}

			if caps, ok := m.ScannerCaps[s.Device]; ok {
				lines = append(lines, "")
				lines = append(lines, hStyle.Render("Capabilities"))
				adfStr := "No (Flatbed only)"
				if caps.HasADF {
					adfStr = "Yes (Automatic Document Feeder supported)"
				}
				lines = append(lines, fmt.Sprintf("ADF:         %s", adfStr))
				if len(caps.Resolutions) > 0 {
					var resStrs []string
					for _, r := range caps.Resolutions {
						resStrs = append(resStrs, fmt.Sprintf("%d", r))
					}
					lines = append(lines, fmt.Sprintf("DPI:         %s (Default: %d)", strings.Join(resStrs, ", "), caps.DefaultResolution))
				}
				if len(caps.Sources) > 0 {
					lines = append(lines, fmt.Sprintf("Sources:     %s", strings.Join(caps.Sources, ", ")))
				}
				if len(caps.Modes) > 0 {
					lines = append(lines, fmt.Sprintf("Modes:       %s", strings.Join(caps.Modes, ", ")))
				}
			}
		} else {
			lines = append(lines, dimStyle.Render("No scanner selected."))
		}

	default:
		lines = append(lines, dimStyle.Render("Select a panel (1-3) to view details."))
	}

	// Apply details scrolling
	start := min(m.DetailsScroll, max(0, len(lines)-innerH))
	end := min(len(lines), start+innerH)
	visible := lines[start:end]

	footer := ""
	if len(lines) > innerH {
		footer = fmt.Sprintf("%d/%d", start+1, len(lines))
	}

	return panel.Frame("[0] Details", strings.Join(visible, "\n"), innerW, innerH, m.Focus == statusbar.PanelDetails, footer)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}
