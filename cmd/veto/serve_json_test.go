package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServeJSONStopsOnSIGTERMWithOpenStdin(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "veto")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	build.Dir = "."
	require.NoError(t, build.Run())

	config, err := filepath.Abs("../../testdata/veto.yaml")
	require.NoError(t, err)
	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = stdinR.Close(); _ = stdinW.Close() })

	cmd := exec.CommandContext(t.Context(), bin, "serve", "--json", "--config", config)
	cmd.Stdin = stdinR
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	require.NoError(t, stdinR.Close())

	_, err = io.WriteString(stdinW, "{\"search\":{\"query\":\"order\"}}\n")
	require.NoError(t, err)
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, line, "orders")

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("serve --json stayed up after SIGTERM with stdin still open")
	}
}
