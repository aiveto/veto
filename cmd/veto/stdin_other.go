//go:build !unix

package main

import (
	"context"
	"io"
	"os"
)

func jsonInput(ctx context.Context, f *os.File) io.Reader {
	if f == nil {
		f = os.Stdin
	}
	stop := closeOnDone(ctx, f)
	return cancelRead{done: ctx.Done(), r: f, stop: stop}
}

type cancelRead struct {
	done <-chan struct{}
	r    io.Reader
	stop func()
}

func (c cancelRead) Read(p []byte) (int, error) {
	select {
	case <-c.done:
		c.stop()
		return 0, context.Canceled
	default:
	}
	n, err := c.r.Read(p)
	if err != nil {
		c.stop()
	}
	return n, err
}
