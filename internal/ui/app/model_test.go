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
