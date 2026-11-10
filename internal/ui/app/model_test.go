package app_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tkilb/lazyprint/internal/config"
	"github.com/tkilb/lazyprint/internal/cups"
	"github.com/tkilb/lazyprint/internal/runner"
	"github.com/tkilb/lazyprint/internal/sane"
	"github.com/tkilb/lazyprint/internal/ui/app"
	"github.com/tkilb/lazyprint/internal/ui/statusbar"
)

func TestModel_NavigationAndFocus(t *testing.T) {
	cfg := config.Default()
	mock := runner.NewMockRunner()
	m := app.NewModel(cfg, mock)

	assert.Equal(t, statusbar.PanelPrinters, m.Focus)

	// Direct number keys
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	assert.Equal(t, statusbar.PanelJobs, newM.(app.Model).Focus)

	newM, _ = newM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	assert.Equal(t, statusbar.PanelScanners, newM.(app.Model).Focus)

	newM, _ = newM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	assert.Equal(t, statusbar.PanelDetails, newM.(app.Model).Focus)

	newM, _ = newM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	assert.Equal(t, statusbar.PanelPrinters, newM.(app.Model).Focus)

	// Tab cycling
	newM, _ = newM.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, statusbar.PanelJobs, newM.(app.Model).Focus)

	newM, _ = newM.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, statusbar.PanelPrinters, newM.(app.Model).Focus)

	// Window resize
	newM, _ = newM.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, 120, newM.(app.Model).Width)
	assert.Equal(t, 40, newM.(app.Model).Height)

	// Quit
	_, cmd := newM.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	require.NotNil(t, cmd)
}

func TestModel_ViewRenderingWithData(t *testing.T) {
	cfg := config.Default()
	mock := runner.NewMockRunner()
	m := app.NewModel(cfg, mock)
	m.Width = 100
	m.Height = 30
	m.LoadingPrinters = false
	m.LoadingJobs = false
	m.LoadingScanners = false

	m.Printers = []cups.Printer{
		{
			Name:          "Brother_Printer",
			Status:        cups.StatusIdle,
			IsDefault:     true,
			StatusMessage: "Waiting for job to complete.",
			DeviceURI:     "ipp://brother.local",
		},
	}

	m.Jobs = []cups.Job{
		{
			ID:          "Brother_Printer-42",
			Printer:     "Brother_Printer",
			JobNumber:   42,
			User:        "tylerkilburn",
			Size:        1024,
			SubmittedAt: "12:00:00",
		},
	}

	m.Scanners = []sane.Device{
		{
			Device: "airscan:e0:Brother",
			Vendor: "eSCL",
			Model:  "Brother HL-L2395DW series",
			Type:   "flatbed scanner",
		},
	}

	view := m.View()
	assert.Contains(t, view, "[1] Printers")
	assert.Contains(t, view, "[2] Jobs")
	assert.Contains(t, view, "[3] Scanners")
	assert.Contains(t, view, "[0] Details")
	assert.Contains(t, view, "Brother_Printer")

	// Focusing Jobs shows full job ID in Details panel
	m.Focus = statusbar.PanelJobs
	viewJobs := m.View()
	assert.Contains(t, viewJobs, "Brother_Printer-42")

	// Focusing Scanners reveals full un-truncated details in Panel 0
	m.Focus = statusbar.PanelScanners
	viewScanners := m.View()
	assert.Contains(t, viewScanners, "HL-L2395DW series")
	assert.Contains(t, viewScanners, "flatbed scanner")
}

func TestModel_PrinterActions_And_Modals(t *testing.T) {
	cfg := config.Default()
	mock := runner.NewMockRunner()
	mock.Register("lpoptions -d Brother_Printer", []byte(""), nil)
	mock.Register("cupsdisable Brother_Printer", []byte(""), nil)
	mock.Register("lpadmin -x Brother_Printer", []byte(""), nil)
	mock.Register("lpadmin -p New_Brother -E -v ipp://brother.local -m everywhere", []byte(""), nil)

	m := app.NewModel(cfg, mock)
	m.Focus = statusbar.PanelPrinters
	m.Printers = []cups.Printer{
		{
			Name:      "Brother_Printer",
			Status:    cups.StatusIdle,
			Enabled:   true,
			DeviceURI: "ipp://brother.local",
			IsDefault: false,
		},
	}

	// 1. Set default ('d')
	mUpdated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, cmd)
	m = mUpdated.(app.Model)

	// 2. Pause queue (' ')
	mUpdated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	require.NotNil(t, cmd)
	m = mUpdated.(app.Model)

	// 3. Delete queue prompt ('x')
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = mUpdated.(app.Model)
	assert.Contains(t, m.View(), "Delete printer queue")

	// Cancel deletion ('n')
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = mUpdated.(app.Model)
	assert.NotContains(t, m.View(), "Delete printer queue")

	// Confirm deletion ('x' then 'y')
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = mUpdated.(app.Model)
	mUpdated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	require.NotNil(t, cmd)
	m = mUpdated.(app.Model)

	// 4. Rename prompt ('R')
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	m = mUpdated.(app.Model)
	assert.Contains(t, m.View(), "Rename Printer")

	// Cancel rename ('esc')
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = mUpdated.(app.Model)
	assert.NotContains(t, m.View(), "Rename Printer")
}

func TestModel_JobActions_And_Modals(t *testing.T) {
	cfg := config.Default()
	mock := runner.NewMockRunner()
	mock.Register("cancel Brother_Printer-42", []byte(""), nil)
	mock.Register("cancel -a", []byte(""), nil)

	m := app.NewModel(cfg, mock)
	m.Focus = statusbar.PanelJobs
	m.Jobs = []cups.Job{
		{
			ID:      "Brother_Printer-42",
			Printer: "Brother_Printer",
		},
	}

	// 1. Cancel job confirmation ('x')
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = mUpdated.(app.Model)
	assert.Contains(t, m.View(), "Cancel print job Brother_Printer-42?")

	// Confirm cancel ('y')
	mUpdated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	require.NotNil(t, cmd)
	m = mUpdated.(app.Model)

	// 2. Purge jobs confirmation ('X')
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = mUpdated.(app.Model)
	assert.Contains(t, m.View(), "Purge all active print jobs")

	// Confirm purge ('y')
	mUpdated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	require.NotNil(t, cmd)
}
