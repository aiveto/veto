package memory

import (
	"bufio"
	"context"
	"encoding/json"
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
		return nil, fmt.Errorf("memory file required")
	}
	l := &Log{path: path, inner: NewLocalMap()}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, fmt.Errorf("read memory: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var item Item
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, fmt.Errorf("parse memory: %w", err)
		}
		if err := l.inner.Store(context.Background(), item); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	return l, nil
}

func (l *Log) Store(ctx context.Context, item Item) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.inner.Store(ctx, item); err != nil {
		return err
	}
	return l.rewrite()
}

func (l *Log) Search(ctx context.Context, query string) ([]Item, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Search(ctx, query)
}

func (l *Log) Recent(ctx context.Context, n int) ([]Item, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Recent(ctx, n)
}

func (l *Log) Delete(ctx context.Context, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.inner.Delete(ctx, id); err != nil {
		return err
	}
	return l.rewrite()
}

func (l *Log) rewrite() error {
	items, err := l.inner.Recent(context.Background(), 1<<30)
	if err != nil {
		return err
	}
	f, err := os.Create(l.path)
	if err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return fmt.Errorf("write memory: %w", err)
		}
	}
	return nil
}
