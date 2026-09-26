package model_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/model"
)

func TestScriptedIgnoresMessageWhenPackOmitsIt(t *testing.T) {
	got, err := model.NewScripted().Complete(context.Background(), model.Request{
		UserMessage: "delete asset 123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.OperationID != "" {
		t.Fatalf("expected no operation without the pack, got %q", got.OperationID)
	}
	got, err = model.NewScripted().Complete(context.Background(), model.Request{
		UserMessage: "delete asset 123",
		Context:     "user: delete asset 123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.OperationID != "assets.delete" {
		t.Fatalf("expected assets.delete, got %q", got.OperationID)
	}
}
