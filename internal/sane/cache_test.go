package sane_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tkilb/lazyprint/internal/sane"
)

func TestScannerCache_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "lazyprint", "scanners.json")
	now := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)

	original := sane.ScannerCache{
		UpdatedAt: now,
		Devices: []sane.Device{
			{
				Device: "airscan:e0:Brother HL-L2395DW series",
				Vendor: "eSCL",
				Model:  "Brother HL-L2395DW series",
				Type:   "flatbed scanner",
			},
		},
		Capabilities: map[string]sane.Capabilities{
			"airscan:e0:Brother HL-L2395DW series": {
				Resolutions:       []int{100, 150, 200, 300, 600},
				DefaultResolution: 300,
				Sources:           []string{"Flatbed"},
				DefaultSource:     "Flatbed",
				HasADF:            false,
				Modes:             []string{"Color", "Gray"},
				DefaultMode:       "Color",
			},
		},
	}

	require.NoError(t, sane.SaveCache(cachePath, original))

	loaded, err := sane.LoadCache(cachePath)
	require.NoError(t, err)
	assert.True(t, loaded.UpdatedAt.Equal(now))
	assert.Equal(t, original.Devices, loaded.Devices)
	assert.Equal(t, original.Capabilities, loaded.Capabilities)

	// Freshness checks
	assert.True(t, loaded.IsFresh(sane.DefaultCacheTTL, now.Add(1*time.Hour)))
	assert.False(t, loaded.IsFresh(sane.DefaultCacheTTL, now.Add(25*time.Hour)))
}

func TestScannerCache_MissingOrEmpty(t *testing.T) {
	_, err := sane.LoadCache("")
	assert.Error(t, err)

	_, err = sane.LoadCache(filepath.Join(t.TempDir(), "nonexistent.json"))
	assert.Error(t, err)

	empty := sane.ScannerCache{UpdatedAt: time.Now()}
	assert.False(t, empty.IsFresh(sane.DefaultCacheTTL, time.Now()))
}
