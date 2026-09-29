package agent_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/agent"
)

func TestScriptedIgnoresMessageWhenPackOmitsIt(t *testing.T) {
	got, err := agent.NewScripted().Complete(context.Background(), agent.Request{
		UserMessage: "delete asset 123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.OperationID != "" {
		t.Fatalf("expected no operation without the pack, got %q", got.OperationID)
	}
	got, err = agent.NewScripted().Complete(context.Background(), agent.Request{
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
