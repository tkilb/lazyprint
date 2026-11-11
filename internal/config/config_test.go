package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tkilb/lazyprint/internal/config"
)

func TestConfig_DefaultValues(t *testing.T) {
	cfg := config.Default()

	assert.Equal(t, 300, cfg.Scan.DefaultResolution)
	assert.Equal(t, "png", cfg.Scan.DefaultFormat)
	assert.Equal(t, "flatbed", cfg.Scan.DefaultSource)
	assert.True(t, cfg.Scan.AutoCrop)
	assert.Equal(t, 1, cfg.Print.DefaultCopies)
	assert.Equal(t, "off", cfg.Print.DefaultDuplex)
	assert.True(t, cfg.UI.Mouse)

	home, _ := os.UserHomeDir()
	if home != "" {
		assert.Equal(t, filepath.Join(home, "Scans"), cfg.Scan.SaveDir)
	}
}

func TestConfig_LoadFromReader(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		validate func(t *testing.T, cfg config.Config)
	}{
		{
			name: "empty yaml uses defaults",
			yaml: "",
			validate: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, 300, cfg.Scan.DefaultResolution)
				assert.Equal(t, "png", cfg.Scan.DefaultFormat)
			},
		},
		{
			name: "overriding scan and print options",
			yaml: `
scan:
  save_dir: /custom/scans
  default_resolution: 600
  default_format: pdf
  default_source: adf
print:
  default_copies: 2
  default_duplex: long-edge
ui:
  mouse: false
`,
			validate: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, "/custom/scans", cfg.Scan.SaveDir)
				assert.Equal(t, 600, cfg.Scan.DefaultResolution)
				assert.Equal(t, "pdf", cfg.Scan.DefaultFormat)
				assert.Equal(t, "adf", cfg.Scan.DefaultSource)
				assert.Equal(t, 2, cfg.Print.DefaultCopies)
				assert.Equal(t, "long-edge", cfg.Print.DefaultDuplex)
				assert.False(t, cfg.UI.Mouse)
			},
		},
		{
			name: "partial overrides retain other defaults",
			yaml: `
scan:
  default_resolution: 150
`,
			validate: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, 150, cfg.Scan.DefaultResolution)
				assert.Equal(t, "png", cfg.Scan.DefaultFormat)
				assert.Equal(t, 1, cfg.Print.DefaultCopies)
				assert.True(t, cfg.UI.Mouse)
			},
		},
		{
			name: "mime type normalization for default_format",
			yaml: `
scan:
  default_format: image/png
`,
			validate: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, "png", cfg.Scan.DefaultFormat)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.LoadFromReader(strings.NewReader(tc.yaml))
			require.NoError(t, err)
			tc.validate(t, cfg)
		})
	}
}

func TestConfig_LoadFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := `
scan:
  save_dir: /tmp/scans
  default_resolution: 1200
`
	err := os.WriteFile(configPath, []byte(content), 0o600)
	require.NoError(t, err)

	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/scans", cfg.Scan.SaveDir)
	assert.Equal(t, 1200, cfg.Scan.DefaultResolution)
	assert.Equal(t, "png", cfg.Scan.DefaultFormat)
}

func TestConfig_NormalizeFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"image/png", "png"},
		{"png", "png"},
		{".png", "png"},
		{"PNG", "png"},
		{"application/pdf", "pdf"},
		{"pdf", "pdf"},
		{".pdf", "pdf"},
		{"PDF", "pdf"},
		{"image/jpeg", "jpeg"},
		{"image/jpg", "jpeg"},
		{"jpeg", "jpeg"},
		{"jpg", "jpeg"},
		{".jpg", "jpeg"},
		{"image/tiff", "tiff"},
		{"tiff", "tiff"},
		{".tif", "tiff"},
		{"image/x-portable-anymap", "pnm"},
		{"pnm", "pnm"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, config.NormalizeFormat(tc.input))
		})
	}
}

func TestConfig_ExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, "Documents"), config.ExpandHome("~/Documents"))
	assert.Equal(t, home, config.ExpandHome("~"))
	assert.Equal(t, "/var/log", config.ExpandHome("/var/log"))
}
