// Package cups provides drivers and parsers for CUPS printing system CLI tools
// (lpstat, cancel, lpoptions). All external commands execute via runner.Runner
// to maintain hardware isolation in automated tests.
package cups

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tkilb/lazyprint/internal/runner"
)

// PrinterStatus represents the operational state of a printer queue.
type PrinterStatus string

const (
	StatusIdle       PrinterStatus = "idle"
	StatusProcessing PrinterStatus = "processing"
	StatusDisabled   PrinterStatus = "disabled"
	StatusUnknown    PrinterStatus = "unknown"
)

// Printer represents a configured CUPS printer destination.
type Printer struct {
	Name          string        `json:"name"`
	Status        PrinterStatus `json:"status"`
	StatusMessage string        `json:"status_message"`
	Enabled       bool          `json:"enabled"`
	Since         string        `json:"since"`
	IsDefault     bool          `json:"is_default"`
	DeviceURI     string        `json:"device_uri"`
}

// Job represents a print queue job from lpstat -o.
type Job struct {
	ID          string `json:"id"`
	Printer     string `json:"printer"`
	JobNumber   int    `json:"job_number"`
	User        string `json:"user"`
	Size        int64  `json:"size"`
	SubmittedAt string `json:"submitted_at"`
}

// PrinterOption represents a configurable PPD option from lpoptions -l.
type PrinterOption struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Choices     []string `json:"choices"`
	Default     string   `json:"default"`
}

// Client wraps CUPS CLI interactions using runner.Runner.
type Client struct {
	r runner.Runner
}

// NewClient returns a new CUPS Client.
func NewClient(r runner.Runner) *Client {
	return &Client{r: r}
}

// GetPrinters fetches and parses configured printers via lpstat -p -d and lpstat -v.
func (c *Client) GetPrinters(ctx context.Context) ([]Printer, error) {
	out, err := c.r.Run(ctx, "lpstat", "-p", "-d")
	if err != nil {
		return nil, fmt.Errorf("lpstat -p -d failed: %w", err)
	}

	printers, defaultDest, err := ParsePrinters(string(out))
	if err != nil {
		return nil, err
	}

	// Fetch device URIs via lpstat -v (best effort, do not fail entire call if unavailable)
	vOut, vErr := c.r.Run(ctx, "lpstat", "-v")
	if vErr == nil {
		uris := ParseDeviceURIs(string(vOut))
		for i := range printers {
			if uri, ok := uris[printers[i].Name]; ok {
				printers[i].DeviceURI = uri
			}
			if defaultDest != "" && printers[i].Name == defaultDest {
				printers[i].IsDefault = true
			}
		}
	} else if defaultDest != "" {
		for i := range printers {
			if printers[i].Name == defaultDest {
				printers[i].IsDefault = true
			}
		}
	}

	return printers, nil
}

// GetJobs fetches and parses active print jobs via lpstat -o.
func (c *Client) GetJobs(ctx context.Context) ([]Job, error) {
	out, err := c.r.Run(ctx, "lpstat", "-o")
	if err != nil {
		// If there are no jobs or lpstat returns non-zero when empty, handle gracefully
		if strings.Contains(string(out), "No destinations added") {
			return []Job{}, nil
		}
		return nil, fmt.Errorf("lpstat -o failed: %w", err)
	}

	return ParseJobs(string(out))
}

// GetOptions fetches PPD options for a printer via lpoptions -p <printer> -l.
func (c *Client) GetOptions(ctx context.Context, printer string) ([]PrinterOption, error) {
	out, err := c.r.Run(ctx, "lpoptions", "-p", printer, "-l")
	if err != nil {
		return nil, fmt.Errorf("lpoptions -p %s -l failed: %w", printer, err)
	}

	return ParseOptions(string(out))
}

// CancelJob cancels a specific job by ID via cancel <jobID>.
func (c *Client) CancelJob(ctx context.Context, jobID string) error {
	_, err := c.r.Run(ctx, "cancel", jobID)
	if err != nil {
		return fmt.Errorf("cancel %s failed: %w", jobID, err)
	}
	return nil
}

// CancelAll cancels all jobs in a queue via cancel -a [printer].
func (c *Client) CancelAll(ctx context.Context, printer string) error {
	args := []string{"-a"}
	if printer != "" {
		args = append(args, printer)
	}
	_, err := c.r.Run(ctx, "cancel", args...)
	if err != nil {
		return fmt.Errorf("cancel %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

// SetDefault sets the default destination via lpoptions -d <printer>.
func (c *Client) SetDefault(ctx context.Context, printer string) error {
	_, err := c.r.Run(ctx, "lpoptions", "-d", printer)
	if err != nil {
		return fmt.Errorf("lpoptions -d %s failed: %w", printer, err)
	}
	return nil
}

// PausePrinter pauses a printer queue via cupsdisable <printer>.
func (c *Client) PausePrinter(ctx context.Context, printer string) error {
	_, err := c.r.Run(ctx, "cupsdisable", printer)
	if err != nil {
		return fmt.Errorf("cupsdisable %s failed: %w", printer, err)
	}
	return nil
}

// ResumePrinter resumes a printer queue via cupsenable <printer>.
func (c *Client) ResumePrinter(ctx context.Context, printer string) error {
	_, err := c.r.Run(ctx, "cupsenable", printer)
	if err != nil {
		return fmt.Errorf("cupsenable %s failed: %w", printer, err)
	}
	return nil
}

// DeletePrinter deletes a printer queue via lpadmin -x <printer>.
func (c *Client) DeletePrinter(ctx context.Context, printer string) error {
	_, err := c.r.Run(ctx, "lpadmin", "-x", printer)
	if err != nil {
		return fmt.Errorf("lpadmin -x %s failed: %w", printer, err)
	}
	return nil
}

// RenamePrinter recreates a queue with a new name and removes the old queue.
func (c *Client) RenamePrinter(ctx context.Context, oldName, newName, deviceURI string, isDefault bool) error {
	if newName == "" || newName == oldName {
		return fmt.Errorf("invalid new printer name: %q", newName)
	}
	if deviceURI == "" {
		vOut, err := c.r.Run(ctx, "lpstat", "-v")
		if err == nil {
			uris := ParseDeviceURIs(string(vOut))
			deviceURI = uris[oldName]
		}
	}
	if deviceURI == "" {
		return fmt.Errorf("cannot rename %s: unable to resolve device URI", oldName)
	}

	// 1. Create new queue: lpadmin -p <newName> -E -v <deviceURI> -m everywhere
	_, err := c.r.Run(ctx, "lpadmin", "-p", newName, "-E", "-v", deviceURI, "-m", "everywhere")
	if err != nil {
		return fmt.Errorf("lpadmin create %s failed: %w", newName, err)
	}

	// 2. Transfer default status if old printer was default
	if isDefault {
		_ = c.SetDefault(ctx, newName)
	}

	// 3. Remove old queue: lpadmin -x <oldName>
	_, err = c.r.Run(ctx, "lpadmin", "-x", oldName)
	if err != nil {
		return fmt.Errorf("lpadmin delete %s failed: %w", oldName, err)
	}

	return nil
}

// Regex patterns for parsing lpstat outputs.
var (
	// printer Brother_Printer is idle.  enabled since Tue 10 Nov 2026 12:28:52 PM EST
	// printer HP_Deskjet now printing HP_Deskjet-42.  enabled since ...
	// printer Generic is disabled since ...
	rePrinterLine = regexp.MustCompile(`^printer\s+([^\s]+)\s+(.+)$`)

	// system default destination: Brother_Printer
	reDefaultLine = regexp.MustCompile(`^system default destination:\s*(.+)$`)

	// device for Brother_Printer: ipp://Brother%20HL-L2395DW%20series._ipp._tcp.local/
	reDeviceLine = regexp.MustCompile(`^device for\s+([^:]+):\s*(.+)$`)
)

// ParsePrinters parses the output of `lpstat -p -d`.
// It returns a list of Printer objects, the detected default destination name, and any error.
func ParsePrinters(output string) ([]Printer, string, error) {
	var printers []Printer
	var defaultDest string
	var current *Printer

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if match := reDefaultLine.FindStringSubmatch(trimmed); match != nil {
			defaultDest = strings.TrimSpace(match[1])
			continue
		}

		if match := rePrinterLine.FindStringSubmatch(trimmed); match != nil {
			if current != nil {
				printers = append(printers, *current)
			}
			name := match[1]
			rest := match[2]

			p := Printer{
				Name:    name,
				Status:  StatusUnknown,
				Enabled: strings.Contains(rest, "enabled"),
			}

			if idx := strings.Index(rest, "enabled since "); idx != -1 {
				p.Since = strings.TrimSpace(rest[idx+len("enabled since "):])
			} else if idx := strings.Index(rest, "disabled since "); idx != -1 {
				p.Since = strings.TrimSpace(rest[idx+len("disabled since "):])
			}

			switch {
			case strings.HasPrefix(rest, "is idle"):
				p.Status = StatusIdle
			case strings.HasPrefix(rest, "now printing"):
				p.Status = StatusProcessing
			case strings.HasPrefix(rest, "is disabled") || strings.Contains(rest, "disabled"):
				p.Status = StatusDisabled
			}

			current = &p
			continue
		}

		// Indented status message under current printer
		if current != nil && (strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "  ")) {
			if current.StatusMessage == "" {
				current.StatusMessage = trimmed
			} else {
				current.StatusMessage += " " + trimmed
			}
		}
	}

	if current != nil {
		printers = append(printers, *current)
	}

	for i := range printers {
		if defaultDest != "" && printers[i].Name == defaultDest {
			printers[i].IsDefault = true
		}
	}

	return printers, defaultDest, scanner.Err()
}

// ParseDeviceURIs parses the output of `lpstat -v`.
func ParseDeviceURIs(output string) map[string]string {
	uris := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if match := reDeviceLine.FindStringSubmatch(line); match != nil {
			uris[strings.TrimSpace(match[1])] = strings.TrimSpace(match[2])
		}
	}
	return uris
}

// ParseJobs parses the output of `lpstat -o`.
// Format: `<job-id> <user> <size> <submitted_at>`
// e.g.: `Brother_Printer-42      tylerkilburn     1024   Tue 10 Nov 2026 12:28:52 PM EST`
func ParseJobs(output string) ([]Job, error) {
	var jobs []Job
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		jobID := fields[0]
		user := fields[1]
		sizeStr := fields[2]
		submittedAt := strings.Join(fields[3:], " ")

		size, _ := strconv.ParseInt(sizeStr, 10, 64)

		printerName := jobID
		jobNumber := 0
		if dashIdx := strings.LastIndex(jobID, "-"); dashIdx != -1 {
			printerName = jobID[:dashIdx]
			if num, err := strconv.Atoi(jobID[dashIdx+1:]); err == nil {
				jobNumber = num
			}
		}

		jobs = append(jobs, Job{
			ID:          jobID,
			Printer:     printerName,
			JobNumber:   jobNumber,
			User:        user,
			Size:        size,
			SubmittedAt: submittedAt,
		})
	}

	return jobs, scanner.Err()
}

// ParseOptions parses `lpoptions -l` output.
// Lines are formatted as: `Keyword/Description: Choice1 *Choice2 Choice3`
func ParseOptions(output string) ([]PrinterOption, error) {
	var options []PrinterOption
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}

		header := line[:colonIdx]
		choicesStr := strings.TrimSpace(line[colonIdx+1:])

		name := header
		desc := header
		if slashIdx := strings.Index(header, "/"); slashIdx != -1 {
			name = header[:slashIdx]
			desc = header[slashIdx+1:]
		}

		rawChoices := strings.Fields(choicesStr)
		choices := make([]string, 0, len(rawChoices))
		defaultChoice := ""

		for _, rc := range rawChoices {
			if strings.HasPrefix(rc, "*") {
				clean := strings.TrimPrefix(rc, "*")
				defaultChoice = clean
				choices = append(choices, clean)
			} else {
				choices = append(choices, rc)
			}
		}

		options = append(options, PrinterOption{
			Name:        name,
			Description: desc,
			Choices:     choices,
			Default:     defaultChoice,
		})
	}

	return options, scanner.Err()
}
