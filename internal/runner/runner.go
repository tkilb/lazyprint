// Package runner provides an abstraction over operating system command execution.
// All interactions with external system binaries (lpstat, scanimage, lp, etc.)
// must go through this interface to ensure complete hardware isolation during testing.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// Runner abstracts command execution for testability and hardware isolation.
type Runner interface {
	// Run executes name with args and returns standard output.
	// If the command fails, the returned error includes stderr output.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)

	// RunCommand executes a fully-specified ExecCommand.
	RunCommand(ctx context.Context, cmd ExecCommand) (Result, error)

	// LookPath searches for an executable in the system PATH.
	LookPath(file string) (string, error)
}

// ExecCommand describes an external command invocation.
type ExecCommand struct {
	Name  string
	Args  []string
	Stdin io.Reader
	Dir   string
	Env   []string
}

// String returns a shell-like representation of the command.
func (c ExecCommand) String() string {
	parts := append([]string{c.Name}, c.Args...)
	return strings.Join(parts, " ")
}

// Result holds the outputs and exit status of a command execution.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// RealRunner executes commands using os/exec.
type RealRunner struct{}

// NewRealRunner returns a production Runner backed by os/exec.
func NewRealRunner() *RealRunner {
	return &RealRunner{}
}

// Run executes name with args and returns stdout.
func (r *RealRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	res, err := r.RunCommand(ctx, ExecCommand{Name: name, Args: args})
	if err != nil {
		return res.Stdout, err
	}
	return res.Stdout, nil
}

// RunCommand executes the command described by spec.
func (r *RealRunner) RunCommand(ctx context.Context, spec ExecCommand) (Result, error) {
	cmd := exec.CommandContext(ctx, spec.Name, spec.Args...)
	if spec.Stdin != nil {
		cmd.Stdin = spec.Stdin
	}
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	if len(spec.Env) > 0 {
		cmd.Env = spec.Env
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	res := Result{
		Stdout:   stdoutBuf.Bytes(),
		Stderr:   stderrBuf.Bytes(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
		} else {
			res.ExitCode = -1
		}
		trimmedStderr := strings.TrimSpace(stderrBuf.String())
		if trimmedStderr != "" {
			return res, fmt.Errorf("%w: %s", err, trimmedStderr)
		}
		return res, err
	}

	return res, nil
}

// LookPath searches for an executable in PATH.
func (r *RealRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// MockResponse represents a canned response for MockRunner.
type MockResponse struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

// MockRunner is an in-memory Runner for unit tests.
type MockRunner struct {
	mu        sync.Mutex
	calls     []ExecCommand
	responses map[string]MockResponse
	lookPaths map[string]string
	defaultFn func(ctx context.Context, cmd ExecCommand) (Result, error)
}

// NewMockRunner creates an empty MockRunner.
func NewMockRunner() *MockRunner {
	return &MockRunner{
		responses: make(map[string]MockResponse),
		lookPaths: make(map[string]string),
	}
}

// Register registers a canned response for a full command string (e.g. "lpstat -p -d")
// or binary name (e.g. "lpstat").
func (m *MockRunner) Register(command string, stdout []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[command] = MockResponse{
		Stdout: stdout,
		Err:    err,
	}
}

// RegisterResult registers a full MockResponse for a command string.
func (m *MockRunner) RegisterResult(command string, res MockResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[command] = res
}

// RegisterLookPath registers a fake path for LookPath.
func (m *MockRunner) RegisterLookPath(file, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookPaths[file] = path
}

// SetDefaultHandler sets a fallback handler when no canned response matches.
func (m *MockRunner) SetDefaultHandler(fn func(ctx context.Context, cmd ExecCommand) (Result, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultFn = fn
}

// Calls returns a copy of all recorded calls.
func (m *MockRunner) Calls() []ExecCommand {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ExecCommand, len(m.calls))
	copy(out, m.calls)
	return out
}

// Run implements Runner.
func (m *MockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	res, err := m.RunCommand(ctx, ExecCommand{Name: name, Args: args})
	return res.Stdout, err
}

// RunCommand implements Runner.
func (m *MockRunner) RunCommand(ctx context.Context, spec ExecCommand) (Result, error) {
	m.mu.Lock()
	m.calls = append(m.calls, spec)
	cmdKey := spec.String()
	res, ok := m.responses[cmdKey]
	if !ok {
		// Try matching just the binary name
		res, ok = m.responses[spec.Name]
	}
	defaultFn := m.defaultFn
	m.mu.Unlock()

	if ok {
		return Result{
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			ExitCode: res.ExitCode,
		}, res.Err
	}

	if defaultFn != nil {
		return defaultFn(ctx, spec)
	}

	return Result{ExitCode: -1}, fmt.Errorf("mock runner: no response registered for command: %s", cmdKey)
}

// LookPath implements Runner.
func (m *MockRunner) LookPath(file string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path, ok := m.lookPaths[file]; ok {
		return path, nil
	}
	return "", exec.ErrNotFound
}
