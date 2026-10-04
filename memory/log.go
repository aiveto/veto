package memory

import (
	"bufio"
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/aiveto/veto/internal/atomicfile"
)

const maxItemBytes = 1 << 20

// Log is an off-by-default file of turns. The process still answers from memory after load.
type Log struct {
	path  string
	mu    sync.Mutex
	inner *Map
}

func NewLog(path string) (*Log, error) {
	if path == "" {
		return nil, errors.New("memory file required")
	}
	l := &Log{path: path, inner: New()}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, fmt.Errorf("read memory: %w", err)
	}
	readErr := readItems(f, l)
	if err := f.Close(); err != nil && readErr == nil {
		readErr = fmt.Errorf("read memory: %w", err)
	}
	if readErr != nil {
		return nil, readErr
	}
	return l, nil
}

func readItems(f *os.File, l *Log) error {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxItemBytes+1)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var item Item
		if err := jsonv2.Unmarshal(line, &item); err != nil {
			return fmt.Errorf("parse memory: %w", err)
		}
		if err := l.inner.Store(context.Background(), item); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read memory: %w", err)
	}
	return nil
}

func (l *Log) Store(ctx context.Context, item Item) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := jsonv2.Marshal(item)
	if err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	if len(raw) > maxItemBytes {
		return fmt.Errorf("memory item exceeds %d bytes", maxItemBytes)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.inner.Store(ctx, item); err != nil {
		return err
	}
	return l.rewrite(ctx)
}

func (l *Log) Search(ctx context.Context, query string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Search(ctx, query)
}

func (l *Log) Recent(ctx context.Context, n int) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Recent(ctx, n)
}

func (l *Log) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.inner.Delete(ctx, id); err != nil {
		return err
	}
	return l.rewrite(ctx)
}

func (l *Log) rewrite(ctx context.Context) error {
	items, err := l.inner.Recent(ctx, 1<<30)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, item := range items {
		raw, err := jsonv2.Marshal(item)
		if err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	if err := atomicfile.Write(l.path, buf.Bytes(), 0); err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	return nil
}
