package main

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseOnDoneUnblocksABlockedRead(t *testing.T) {
	r, w := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	stop := closeOnDone(ctx, r)
	t.Cleanup(stop)
	t.Cleanup(func() { _ = w.Close() })
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(r)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("read stayed blocked after cancel")
	}
}
