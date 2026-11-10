// Package config handles reading and parsing lazyprint's configuration.
// By default, it looks for ~/.config/lazyprint.yaml (fallback: ~/.config/lazyprint/config.yaml).
// If no configuration file exists, safe defaults are provided.
package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Primary and fallback config paths relative to user's home directory.
const (
	primaryConfigFile  = ".config/lazyprint.yaml"
	fallbackConfigFile = ".config/lazyprint/config.yaml"
)

// Default settings.
const (
	DefaultScanDirResolution = 300
	DefaultScanFormat        = "pdf"
	DefaultScanSource        = "flatbed"
	DefaultPrintCopies       = 1
	DefaultPrintDuplex       = "off"
)

// Config holds the full application configuration.
type Config struct {
	Scan  ScanConfig  `yaml:"scan"`
	Print PrintConfig `yaml:"print"`
	UI    UIConfig    `yaml:"ui"`
}

// ScanConfig defines scanning preferences.
type ScanConfig struct {
	// SaveDir is where scanned documents are written. Defaults to ~/Scans.
	SaveDir string `yaml:"save_dir"`

	// DefaultResolution is the default DPI (e.g. 150, 300, 600). Defaults to 300.
	DefaultResolution int `yaml:"default_resolution"`

	// DefaultFormat is the default file extension / format (pdf, png, jpeg). Defaults to "pdf".
	DefaultFormat string `yaml:"default_format"`

	// DefaultSource is the scan input source (flatbed, adf). Defaults to "flatbed".
	DefaultSource string `yaml:"default_source"`
}

// PrintConfig defines printing preferences.
type PrintConfig struct {
	// DefaultCopies is the default number of copies. Defaults to 1.
	DefaultCopies int `yaml:"default_copies"`

	// DefaultDuplex specifies duplex printing mode ("off", "long-edge", "short-edge"). Defaults to "off".
	DefaultDuplex string `yaml:"default_duplex"`
}

// UIConfig defines TUI behavior and display preferences.
type UIConfig struct {
	// Mouse enables or disables terminal mouse support. Defaults to true.
	Mouse bool `yaml:"mouse"`
}

// Default returns a Config populated with default values.
func Default() Config {
	home, _ := os.UserHomeDir()
	defaultScanDir := "~/Scans"
	if home != "" {
		defaultScanDir = filepath.Join(home, "Scans")
	}

	return Config{
		Scan: ScanConfig{
			SaveDir:           defaultScanDir,
			DefaultResolution: DefaultScanDirResolution,
			DefaultFormat:     DefaultScanFormat,
			DefaultSource:     DefaultScanSource,
		},
		Print: PrintConfig{
			DefaultCopies: DefaultPrintCopies,
			DefaultDuplex: DefaultPrintDuplex,
		},
		UI: UIConfig{
			Mouse: true,
		},
	}
}

// ExpandHome expands a leading "~/" or "~" with the user's home directory.
func ExpandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// FindConfigFile checks standard paths for a config file.
// Returns the first path that exists, or an empty string if none are found.
func FindConfigFile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}

	primary := filepath.Join(home, primaryConfigFile)
	if _, err := os.Stat(primary); err == nil {
		return primary
	}

	fallback := filepath.Join(home, fallbackConfigFile)
	if _, err := os.Stat(fallback); err == nil {
		return fallback
	}

	return ""
}

// Load loads configuration from path. If path is empty, it attempts to discover
// the config file via FindConfigFile. If no config file exists, Default() is returned.
func Load(path string) (Config, error) {
	if path == "" {
		path = FindConfigFile()
	}

	if path == "" {
		return Default(), nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Default(), err
	}
	defer f.Close()

	return LoadFromReader(f)
}

// LoadFromReader parses YAML configuration from an io.Reader and merges onto defaults.
func LoadFromReader(r io.Reader) (Config, error) {
	cfg := Default()
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return cfg, err
	}

	cfg.Scan.SaveDir = ExpandHome(cfg.Scan.SaveDir)
	return cfg, nil
}
