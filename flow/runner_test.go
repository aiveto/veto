package flow_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/flow"
)

func TestStepOutputFeedsTheNextParameter(t *testing.T) {
	def := &flow.Definition{Name: "get-team", Steps: []flow.Step{
		{Operation: "assets.get", Output: "teamsId", To: "id"},
		{Operation: "teams.get"},
	}}
	var got []map[string]string
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		got = append(got, clone(params))
		if operationID == "assets.get" {
			return "ok", `{"teamsId":"9"}`, nil
		}
		return "ok", `{"id":"9"}`, nil
	}}
	results, err := runner.Run(context.Background(), def, map[string]string{"id": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || got[1]["id"] != "9" {
		t.Fatalf("results=%v params=%v", results, got)
	}
}

func TestConfirmationStopsTheNextStep(t *testing.T) {
	def := &flow.Definition{Name: "then-delete", Steps: []flow.Step{
		{Operation: "assets.get", Output: "id", To: "id"},
		{Operation: "assets.delete"},
	}}
	var called []string
	runner := flow.Runner{Invoke: func(ctx context.Context, operationID string, params map[string]string, approvalID string) (string, string, error) {
		called = append(called, operationID)
		if operationID == "assets.delete" {
			return "confirmation_required", "", nil
		}
		return "ok", `{"id":"7"}`, nil
	}}
	_, err := runner.Run(context.Background(), def, nil)
	var stopped flow.Stopped
	if err == nil || !asStopped(err, &stopped) || stopped.Operation != "assets.delete" {
		t.Fatalf("err: %v", err)
	}
	if len(called) != 2 || called[1] != "assets.delete" {
		t.Fatalf("called: %v", called)
	}
}

func asStopped(err error, target *flow.Stopped) bool {
	s, ok := err.(flow.Stopped)
	if ok {
		*target = s
	}
	return ok
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
