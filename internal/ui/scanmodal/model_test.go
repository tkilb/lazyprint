package scanmodal_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/tkilb/lazyprint/internal/config"
	"github.com/tkilb/lazyprint/internal/sane"
	"github.com/tkilb/lazyprint/internal/ui/scanmodal"
)

func TestScanModal_NavigationAndRequest(t *testing.T) {
	cfg := config.Default()
	dev := sane.Device{
		Device: "airscan:e0:Brother",
		Model:  "Brother HL-L2395DW",
	}
	caps := sane.Capabilities{
		Resolutions:       []int{150, 300, 600},
		DefaultResolution: 300,
		Sources:           []string{"Flatbed", "ADF"},
		DefaultSource:     "Flatbed",
		HasADF:            true,
	}
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC)

	m := scanmodal.New(dev, caps, cfg, 300, "pdf", "Flatbed", "Color", true, now)

	// Initial request values
	req := m.Request()
	assert.Equal(t, "airscan:e0:Brother", req.Device)
	assert.Equal(t, 300, req.Resolution)
	assert.Equal(t, "Flatbed", req.Source)
	assert.Equal(t, "Color", req.Mode)
	assert.Equal(t, "pdf", req.Format)
	assert.True(t, req.AutoCrop)
	assert.Contains(t, req.OutputPath, "scan_20261110_140000.pdf")

	// Cycle Source right ('l') -> ADF
	m, _, keepOpen := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	assert.True(t, keepOpen)
	assert.Equal(t, "ADF", m.Request().Source)

	// Tab to Resolution
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Cycle Resolution right ('l') -> 600
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	assert.Equal(t, 600, m.Request().Resolution)

	// Tab to Mode
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Cycle Mode right ('l')
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})

	// Tab to Format
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Cycle Format right ('l') -> png
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	assert.Equal(t, "png", m.Request().Format)
	assert.True(t, strings.HasSuffix(m.Request().OutputPath, ".png"))

	// Tab to AutoCrop
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Toggle AutoCrop with space -> false
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	assert.False(t, m.Request().AutoCrop)
	// Toggle AutoCrop with 'l' -> true
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	assert.True(t, m.Request().AutoCrop)

	// Tab to Path
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Escape cancels modal
	_, _, keepOpen = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	assert.False(t, keepOpen)
}

func TestScanModal_View(t *testing.T) {
	cfg := config.Default()
	dev := sane.Device{
		Device: "genesys:libusb",
		Model:  "Canon LiDE 110",
	}
	caps := sane.Capabilities{
		Resolutions: []int{300},
		Sources:     []string{"Flatbed"},
	}
	m := scanmodal.New(dev, caps, cfg, 300, "pdf", "Flatbed", "Color", true, time.Now())
	view := m.View()

	assert.Contains(t, view, "Scan: Canon LiDE 110")
	assert.Contains(t, view, "Source:")
	assert.Contains(t, view, "Flatbed")
	assert.Contains(t, view, "Resolution:")
	assert.Contains(t, view, "300 DPI")
	assert.Contains(t, view, "Color Mode:")
	assert.Contains(t, view, "Auto-Crop:")
	assert.Contains(t, view, "Destination file:")
}

func TestScanModal_RememberedPreferences(t *testing.T) {
	cfg := config.Default()
	dev := sane.Device{Device: "airscan:e0:Brother"}
	caps := sane.Capabilities{
		Resolutions: []int{150, 300, 600},
		Sources:     []string{"Flatbed", "ADF"},
	}

	m := scanmodal.New(dev, caps, cfg, 600, "png", "ADF", "Color", false, time.Now())
	req := m.Request()
	assert.Equal(t, 600, req.Resolution)
	assert.Equal(t, "png", req.Format)
	assert.Equal(t, "ADF", req.Source)
	assert.False(t, req.AutoCrop)

	// Test default format inheritance from config and MIME normalization
	mMime := scanmodal.New(dev, caps, cfg, 300, "image/png", "Flatbed", "Color", true, time.Now())
	assert.Equal(t, "png", mMime.Request().Format)

	mDefault := scanmodal.New(dev, caps, cfg, 0, "", "", "", true, time.Now())
	assert.Equal(t, "png", mDefault.Request().Format)
}
