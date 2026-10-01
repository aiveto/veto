package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// Log is an off-by-default file of turns. The process still answers from memory after load.
type Log struct {
	path  string
	mu    sync.Mutex
	inner *LocalMap
}

func NewLog(path string) (*Log, error) {
	if path == "" {
		return nil, errors.New("memory file required")
	}
	l := &Log{path: path, inner: NewLocalMap()}
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
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var item Item
		if err := json.Unmarshal(line, &item); err != nil {
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
	f, err := os.Create(l.path)
	if err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	enc := json.NewEncoder(f)
	var writeErr error
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			writeErr = fmt.Errorf("write memory: %w", err)
			break
		}
	}
	if err := f.Close(); err != nil && writeErr == nil {
		writeErr = fmt.Errorf("write memory: %w", err)
	}
	return writeErr
}
