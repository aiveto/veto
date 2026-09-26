package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type (
	// Item is one stored memory entry.
	Item struct {
		ID      string
		Content string
		Tags    []string
	}

	// Memory stores and retrieves items across runs.
	Memory interface {
		Store(ctx context.Context, item Item) error
		Search(ctx context.Context, query string) ([]Item, error)
		Delete(ctx context.Context, id string) error
	}

	// LocalMap is an in-memory implementation.
	LocalMap struct {
		mu    sync.RWMutex
		items map[string]Item
	}
)

// NewLocalMap creates an empty memory store.
func NewLocalMap() *LocalMap {
	return &LocalMap{items: map[string]Item{}}
}

func (m *LocalMap) Store(ctx context.Context, item Item) error {
	_ = ctx
	if item.ID == "" {
		return fmt.Errorf("memory item id required")
	}
	m.mu.Lock()
	m.items[item.ID] = item
	m.mu.Unlock()
	return nil
}

func (m *LocalMap) Search(ctx context.Context, query string) ([]Item, error) {
	_ = ctx
	q := strings.ToLower(query)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Item
	for _, it := range m.items {
		if q == "" || strings.Contains(strings.ToLower(it.Content), q) {
			out = append(out, it)
		}
	}
	return out, nil
}

func (m *LocalMap) Delete(ctx context.Context, id string) error {
	_ = ctx
	m.mu.Lock()
	delete(m.items, id)
	m.mu.Unlock()
	return nil
}
