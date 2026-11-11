package app_test

import (
	"context"
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

func TestModel_ScanningActions_And_Modal(t *testing.T) {
	cfg := config.Default()
	mock := runner.NewMockRunner()
	mock.RegisterLookPath("magick", "/usr/bin/magick")
	mock.SetDefaultHandler(func(_ context.Context, _ runner.ExecCommand) (runner.Result, error) {
		return runner.Result{ExitCode: 0}, nil
	})

	m := app.NewModel(cfg, mock)
	m.Focus = statusbar.PanelScanners
	m.Scanners = []sane.Device{
		{
			Device: "airscan:e0:Brother",
			Model:  "Brother HL-L2395DW",
			Type:   "flatbed scanner",
		},
	}
	m.ScannerCaps["airscan:e0:Brother"] = sane.Capabilities{
		Resolutions:   []int{150, 300, 600},
		Sources:       []string{"Flatbed", "ADF"},
		HasADF:        true,
	}

	// 1. Open scan modal ('s')
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = mUpdated.(app.Model)
	assert.True(t, m.ScanningModal)
	assert.Contains(t, m.View(), "Scan: Brother HL-L2395DW")

	// Navigate to resolution and change it with 'l'
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mUpdated.(app.Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = mUpdated.(app.Model)

	// Default format is png; resolution changed to 600
	// 2. Confirm scan in modal ('enter')
	mUpdated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mUpdated.(app.Model)
	assert.False(t, m.ScanningModal)
	assert.True(t, m.Scanning)
	require.NotNil(t, cmd)
	assert.Contains(t, m.View(), "Scanning to")
	assert.Equal(t, 600, m.LastScanResolution)
	assert.Equal(t, "png", m.LastScanFormat)

	// Verify next modal open inherits remembered settings
	m.Scanning = false
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = mUpdated.(app.Model)
	assert.True(t, m.ScanningModal)
	req := m.ScanModal.Request()
	assert.Equal(t, 600, req.Resolution)
	assert.Equal(t, "png", req.Format)

	// Change format to jpeg and resolution to 150, then press Esc
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // resolution
	m = mUpdated.(app.Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")}) // 600 -> 300
	m = mUpdated.(app.Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")}) // 300 -> 150
	m = mUpdated.(app.Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // mode
	m = mUpdated.(app.Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // format
	m = mUpdated.(app.Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}) // png -> jpeg
	m = mUpdated.(app.Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mUpdated.(app.Model)
	assert.False(t, m.ScanningModal)
	assert.Equal(t, 150, m.LastScanResolution)
	assert.Equal(t, "jpeg", m.LastScanFormat)

	// Re-open modal and confirm 150 DPI and jpeg are remembered after Esc
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = mUpdated.(app.Model)
	assert.True(t, m.ScanningModal)
	req = m.ScanModal.Request()
	assert.Equal(t, 150, req.Resolution)
	assert.Equal(t, "jpeg", req.Format)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mUpdated.(app.Model)

	// 3. Quick Scan Flatbed ('f')
	m.Scanning = false
	mUpdated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = mUpdated.(app.Model)
	assert.True(t, m.Scanning)
	require.NotNil(t, cmd)

	// 4. Quick Scan ADF ('a') with ADF-capable scanner
	m.Scanning = false
	mUpdated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = mUpdated.(app.Model)
	assert.True(t, m.Scanning)
	require.NotNil(t, cmd)

	// 5. Quick Scan ADF ('a') on device without ADF
	m.Scanning = false
	m.ScannerCaps["airscan:e0:Brother"] = sane.Capabilities{
		Resolutions: []int{300},
		Sources:     []string{"Flatbed"},
		HasADF:      false,
	}
	mUpdated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = mUpdated.(app.Model)
	assert.False(t, m.Scanning)
	assert.Nil(t, cmd)
	assert.Contains(t, m.StatusBar.Message, "does not support ADF")
}
