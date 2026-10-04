//go:build unix

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

func jsonInput(ctx context.Context, f *os.File) io.Reader {
	if f == nil {
		return os.Stdin
	}
	if err := syscall.SetNonblock(int(f.Fd()), true); err != nil {
		stop := closeOnDone(ctx, f)
		return cancelCloser{done: ctx.Done(), r: f, stop: stop}
	}
	return pollReader{done: ctx.Done(), f: f}
}

type pollReader struct {
	done <-chan struct{}
	f    *os.File
}

func (r pollReader) Read(p []byte) (int, error) {
	for {
		select {
		case <-r.done:
			return 0, context.Canceled
		default:
		}
		n, err := r.f.Read(p)
		if n > 0 {
			return n, err
		}
		if err == nil {
			return 0, io.EOF
		}
		if !isAgain(err) {
			if errors.Is(err, os.ErrClosed) {
				return 0, io.EOF
			}
			return 0, err
		}
		select {
		case <-r.done:
			return 0, context.Canceled
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func isAgain(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}

type cancelCloser struct {
	done <-chan struct{}
	r    io.Reader
	stop func()
}

func (c cancelCloser) Read(p []byte) (int, error) {
	select {
	case <-c.done:
		c.stop()
		return 0, context.Canceled
	default:
	}
	n, err := c.r.Read(p)
	if errors.Is(err, os.ErrClosed) {
		c.stop()
		return n, io.EOF
	}
	return n, err
}
