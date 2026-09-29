package memory_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aiveto/veto/memory"
)

func TestLogKeepsTurnsAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.log")
	log, err := memory.NewLog(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := log.Store(ctx, memory.Item{ID: "1", Content: "delete asset 1"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Store(ctx, memory.Item{ID: "2", Content: "list holdings"}); err != nil {
		t.Fatal(err)
	}
	again, err := memory.NewLog(path)
	if err != nil {
		t.Fatal(err)
	}
	recent, err := again.Recent(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Content != "list holdings" {
		t.Fatalf("recent: %#v", recent)
	}
	found, err := again.Search(ctx, "delete asset")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != "1" {
		t.Fatalf("search: %#v", found)
	}
	if err := again.Delete(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := memory.NewLog(path)
	if err != nil {
		t.Fatal(err)
	}
	found, err = reloaded.Search(ctx, "delete asset")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("deleted turn still in the log: %#v", found)
	}
}
