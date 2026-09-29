package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aiveto/veto/memory"
)

func TestDeleteRemovesIDFromOrder(t *testing.T) {
	ctx := context.Background()
	m := memory.NewLocalMap()
	for _, id := range []string{"a", "b"} {
		if err := m.Store(ctx, memory.Item{ID: id, Content: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Store(ctx, memory.Item{ID: "a", Content: "a"}); err != nil {
		t.Fatal(err)
	}
	recent, err := m.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].ID != "b" || recent[1].ID != "a" {
		t.Fatalf("order: %+v", recent)
	}
}

func TestCanceledContextDoesNoWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := memory.NewLocalMap()
	if err := m.Store(ctx, memory.Item{ID: "a", Content: "a"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("store: %v", err)
	}
	recent, err := m.Recent(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 0 {
		t.Fatalf("stored: %+v", recent)
	}
	if _, err := m.Search(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("search: %v", err)
	}
	if _, err := m.Recent(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("recent: %v", err)
	}
	if err := m.Delete(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("delete: %v", err)
	}
}
