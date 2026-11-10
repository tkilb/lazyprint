package runner_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tkilb/lazyprint/internal/runner"
)

func TestMockRunner_RegistrationAndCalls(t *testing.T) {
	ctx := context.Background()
	m := runner.NewMockRunner()

	m.Register("lpstat -p -d", []byte("printer Brother is idle"), nil)
	m.RegisterResult("cancel 42", runner.MockResponse{
		Stderr:   []byte("permission denied"),
		ExitCode: 1,
		Err:      errors.New("exit status 1"),
	})
	m.RegisterLookPath("scanimage", "/usr/bin/scanimage")

	t.Run("registered command execution", func(t *testing.T) {
		out, err := m.Run(ctx, "lpstat", "-p", "-d")
		require.NoError(t, err)
		assert.Equal(t, "printer Brother is idle", string(out))
	})

	t.Run("registered failure command execution", func(t *testing.T) {
		res, err := m.RunCommand(ctx, runner.ExecCommand{
			Name: "cancel",
			Args: []string{"42"},
		})
		require.Error(t, err)
		assert.Equal(t, 1, res.ExitCode)
		assert.Equal(t, "permission denied", string(res.Stderr))
	})

	t.Run("unregistered command execution without fallback", func(t *testing.T) {
		_, err := m.Run(ctx, "lpadmin", "-x", "old-printer")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no response registered for command: lpadmin -x old-printer")
	})

	t.Run("lookpath registered", func(t *testing.T) {
		path, err := m.LookPath("scanimage")
		require.NoError(t, err)
		assert.Equal(t, "/usr/bin/scanimage", path)
	})

	t.Run("lookpath not found", func(t *testing.T) {
		_, err := m.LookPath("nonexistent-tool")
		require.ErrorIs(t, err, exec.ErrNotFound)
	})

	t.Run("recorded calls", func(t *testing.T) {
		calls := m.Calls()
		require.Len(t, calls, 3)
		assert.Equal(t, "lpstat -p -d", calls[0].String())
		assert.Equal(t, "cancel 42", calls[1].String())
		assert.Equal(t, "lpadmin -x old-printer", calls[2].String())
	})
}

func TestMockRunner_FallbackHandler(t *testing.T) {
	ctx := context.Background()
	m := runner.NewMockRunner()

	m.SetDefaultHandler(func(ctx context.Context, cmd runner.ExecCommand) (runner.Result, error) {
		if cmd.Name == "echo" {
			return runner.Result{
				Stdout:   []byte("echoed: " + cmd.Args[0]),
				ExitCode: 0,
			}, nil
		}
		return runner.Result{ExitCode: 127}, errors.New("command not found")
	})

	out, err := m.Run(ctx, "echo", "hello")
	require.NoError(t, err)
	assert.Equal(t, "echoed: hello", string(out))

	_, err = m.Run(ctx, "unknown-cmd")
	require.Error(t, err)
}
