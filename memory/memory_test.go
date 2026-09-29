package memory_test

import (
	"context"
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
