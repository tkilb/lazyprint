// Package sane provides drivers, parsers, and command builders for SANE scanning
// via the scanimage CLI. All execution routes through runner.Runner for hardware test isolation.
package sane

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tkilb/lazyprint/internal/runner"
)

// Device represents a discovered scanner device from `scanimage -L`.
type Device struct {
	Device string `json:"device"` // e.g. "airscan:e0:Brother HL-L2395DW series"
	Vendor string `json:"vendor"` // e.g. "Brother", "Canon", "eSCL"
	Model  string `json:"model"`  // e.g. "HL-L2395DW series"
	Type   string `json:"type"`   // e.g. "flatbed scanner", "adf scanner", "multi-function peripheral"
}

// Capabilities represents options queried via `scanimage --help -d <device>`.
type Capabilities struct {
	Resolutions       []int    `json:"resolutions"`
	DefaultResolution int      `json:"default_resolution"`
	Sources           []string `json:"sources"`
	DefaultSource     string   `json:"default_source"`
	HasADF            bool     `json:"has_adf"`
	Modes             []string `json:"modes"`
	DefaultMode       string   `json:"default_mode"`
}

// ScanRequest defines parameters for a scan operation.
type ScanRequest struct {
	Device     string `json:"device"`
	Resolution int    `json:"resolution"`
	Source     string `json:"source"`     // e.g. "Flatbed", "ADF"
	Format     string `json:"format"`     // "pdf", "png", "jpeg"
	Mode       string `json:"mode"`       // "Color", "Gray"
	OutputPath string `json:"output_path"`
}

// Client wraps SANE CLI interactions using runner.Runner.
type Client struct {
	r runner.Runner
}

// NewClient returns a new SANE Client.
func NewClient(r runner.Runner) *Client {
	return &Client{r: r}
}

// GetDevices lists scanners via `scanimage -L`.
func (c *Client) GetDevices(ctx context.Context) ([]Device, error) {
	out, err := c.r.Run(ctx, "scanimage", "-L")
	if err != nil {
		return nil, fmt.Errorf("scanimage -L failed: %w", err)
	}

	return ParseDevices(string(out))
}

// GetCapabilities queries device capabilities via `scanimage --help -d <device>`.
func (c *Client) GetCapabilities(ctx context.Context, device string) (Capabilities, error) {
	out, err := c.r.Run(ctx, "scanimage", "--help", "-d", device)
	if err != nil {
		// scanimage --help often exits with status 0 or 1 depending on backend,
		// but stdout contains the options help text.
		if len(out) == 0 {
			return Capabilities{}, fmt.Errorf("scanimage --help -d %s failed: %w", device, err)
		}
	}

	return ParseCapabilities(string(out)), nil
}

// Scan executes a scan with the given request.
func (c *Client) Scan(ctx context.Context, req ScanRequest) error {
	args := BuildScanArgs(req)
	_, err := c.r.Run(ctx, "scanimage", args...)
	if err != nil {
		return fmt.Errorf("scanimage failed: %w", err)
	}
	return nil
}

// BuildScanArgs constructs the argv slice for `scanimage`.
func BuildScanArgs(req ScanRequest) []string {
	var args []string
	if req.Device != "" {
		args = append(args, "-d", req.Device)
	}
	if req.Resolution > 0 {
		args = append(args, "--resolution", strconv.Itoa(req.Resolution))
	}
	if req.Source != "" {
		args = append(args, "--source", req.Source)
	}
	if req.Mode != "" {
		args = append(args, "--mode", req.Mode)
	}
	if req.Format != "" {
		args = append(args, "--format="+strings.ToLower(req.Format))
	}
	if req.OutputPath != "" {
		args = append(args, "-o", req.OutputPath)
	}
	return args
}

// GenerateFilename generates a default autosave filename: scan_YYYYMMDD_HHMMSS.<ext>.
func GenerateFilename(ext string, now time.Time) string {
	cleanExt := strings.TrimPrefix(ext, ".")
	if cleanExt == "" {
		cleanExt = "pdf"
	}
	return fmt.Sprintf("scan_%s.%s", now.Format("20060102_150405"), cleanExt)
}

// BuildDefaultOutputPath combines saveDir and GenerateFilename.
func BuildDefaultOutputPath(saveDir, ext string, now time.Time) string {
	return filepath.Join(saveDir, GenerateFilename(ext, now))
}

var (
	// device `airscan:e0:Brother HL-L2395DW series' is a eSCL Brother HL-L2395DW series ip=192.168.0.246
	// device `genesys:libusb:001:004' is a Canon LiDE 110 flatbed scanner
	reDeviceLine = regexp.MustCompile("device [`'\"]([^`'\"]+)[`'\"]\\s+is\\s+a\\s+(.+)$")

	reResolution = regexp.MustCompile(`--resolution\s+([0-9|]+)dpi(?:\s+\[([0-9]+)\])?`)
	reSource     = regexp.MustCompile(`--source\s+([^\[\r\n]+?)(?:\s+\[([^\]]+)\])?(?:\s|$)`)
	reMode       = regexp.MustCompile(`--mode\s+([^\[\r\n]+?)(?:\s+\[([^\]]+)\])?(?:\s|$)`)
)

// ParseDevices parses the output of `scanimage -L`.
func ParseDevices(output string) ([]Device, error) {
	var devices []Device
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		match := reDeviceLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		devID := match[1]
		desc := strings.TrimSpace(match[2])

		vendor := ""
		model := desc
		devType := ""

		// Common device type suffixes
		for _, suffix := range []string{"flatbed scanner", "adf scanner", "multi-function peripheral", "scanner"} {
			if strings.HasSuffix(strings.ToLower(desc), suffix) {
				devType = suffix
				model = strings.TrimSpace(desc[:len(desc)-len(suffix)])
				break
			}
		}

		// Split vendor and model if vendor is identifiable prefix
		words := strings.Fields(model)
		if len(words) > 1 {
			vendor = words[0]
			model = strings.Join(words[1:], " ")
		}

		devices = append(devices, Device{
			Device: devID,
			Vendor: vendor,
			Model:  model,
			Type:   devType,
		})
	}

	return devices, scanner.Err()
}

// ParseCapabilities parses device capabilities from `scanimage --help -d <device>`.
func ParseCapabilities(output string) Capabilities {
	caps := Capabilities{
		Resolutions:       []int{150, 300, 600},
		DefaultResolution: 300,
		Sources:           []string{"Flatbed"},
		DefaultSource:     "Flatbed",
		Modes:             []string{"Color", "Gray"},
		DefaultMode:       "Color",
	}

	// Resolution matching
	if resMatch := reResolution.FindStringSubmatch(output); resMatch != nil {
		rawRes := strings.Split(resMatch[1], "|")
		var parsed []int
		for _, r := range rawRes {
			if num, err := strconv.Atoi(r); err == nil && num > 0 {
				parsed = append(parsed, num)
			}
		}
		if len(parsed) > 0 {
			caps.Resolutions = parsed
		}
		if len(resMatch) > 2 && resMatch[2] != "" {
			if d, err := strconv.Atoi(resMatch[2]); err == nil {
				caps.DefaultResolution = d
			}
		}
	}

	// Source matching
	if srcMatch := reSource.FindStringSubmatch(output); srcMatch != nil {
		rawSources := strings.Split(srcMatch[1], "|")
		var sources []string
		for _, s := range rawSources {
			s = strings.TrimSpace(s)
			if s != "" {
				sources = append(sources, s)
			}
		}
		if len(sources) > 0 {
			caps.Sources = sources
		}
		if len(srcMatch) > 2 && srcMatch[2] != "" {
			caps.DefaultSource = strings.TrimSpace(srcMatch[2])
		}
	}

	// Mode matching
	if modeMatch := reMode.FindStringSubmatch(output); modeMatch != nil {
		rawModes := strings.Split(modeMatch[1], "|")
		var modes []string
		for _, m := range rawModes {
			m = strings.TrimSpace(m)
			if m != "" {
				modes = append(modes, m)
			}
		}
		if len(modes) > 0 {
			caps.Modes = modes
		}
		if len(modeMatch) > 2 && modeMatch[2] != "" {
			caps.DefaultMode = strings.TrimSpace(modeMatch[2])
		}
	}

	// Detect ADF availability
	for _, s := range caps.Sources {
		lower := strings.ToLower(s)
		if strings.Contains(lower, "adf") || strings.Contains(lower, "feeder") {
			caps.HasADF = true
			break
		}
	}

	return caps
}
