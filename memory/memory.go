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
		Recent(ctx context.Context, n int) ([]Item, error)
		Delete(ctx context.Context, id string) error
	}

	// LocalMap is an in-memory implementation.
	LocalMap struct {
		mu    sync.RWMutex
		items map[string]Item
		order []string
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
	if _, ok := m.items[item.ID]; !ok {
		m.order = append(m.order, item.ID)
	}
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

func (m *LocalMap) Recent(ctx context.Context, n int) ([]Item, error) {
	_ = ctx
	if n <= 0 {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.order))
	for _, id := range m.order {
		if _, ok := m.items[id]; ok {
			ids = append(ids, id)
		}
	}
	if len(ids) > n {
		ids = ids[len(ids)-n:]
	}
	out := make([]Item, 0, len(ids))
	for _, id := range ids {
		out = append(out, m.items[id])
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
