// Package memory stores turns for one run.
package memory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
)

type (
	Item struct {
		ID      string   `json:"ID"`
		Content string   `json:"Content"`
		Tags    []string `json:"Tags"`
	}

	Map struct {
		mu    sync.RWMutex
		items map[string]Item
		order []string
	}
)

func New() *Map {
	return &Map{items: map[string]Item{}}
}

func (m *Map) Store(ctx context.Context, item Item) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if item.ID == "" {
		return errors.New("memory item id required")
	}
	m.mu.Lock()
	if _, ok := m.items[item.ID]; !ok {
		m.order = append(m.order, item.ID)
	}
	m.items[item.ID] = item
	m.mu.Unlock()
	return nil
}

func (m *Map) Search(ctx context.Context, query string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

func (m *Map) Recent(ctx context.Context, n int) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

func (m *Map) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.items, id)
	m.order = slices.DeleteFunc(m.order, func(got string) bool { return got == id })
	m.mu.Unlock()
	return nil
}
