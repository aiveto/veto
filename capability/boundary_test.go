package capability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureExec struct {
	params map[string]string
	sent   bool
	err    error
}

func (c *captureExec) InvokeHTTPResult(_ context.Context, _ *catalog.Operation, params map[string]string) (result.HTTPResult, error) {
	c.params = params
	if c.err != nil {
		return result.HTTPResult{Sent: c.sent}, c.err
	}
	return result.HTTPResult{Status: 204, Code: "ok", HTTP: true, Sent: true, Body: `{"ok":true}`}, nil
}

func testServer(t *testing.T, exec runtime.Executor) *capability.Server {
	t.Helper()
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{
			ID:           "orders.get",
			Method:       "GET",
			PathTemplate: "/orders/{id}",
			Params:       []catalog.Param{{Name: "id", In: "path", Required: true}},
		},
		{
			ID:                   "orders.retire",
			Method:               "DELETE",
			Kind:                 catalog.KindDelete,
			PathTemplate:         "/orders",
			RequiresConfirmation: true,
			Params:               []catalog.Param{{Name: "body", In: "body", Required: true}},
		},
	}}
	cat.Finalize()
	rt := runtime.Runtime{
		Catalog: cat,
		Exec:    exec,
		State:   policy.NewState(),
		Policy:  policy.Builtin{},
		Base:    policy.Builtin{},
		Gate:    &runtime.InvokeGate{Per: 1024},
	}
	return &capability.Server{Catalog: cat, Calls: &rt}
}

func TestParamsKeepLargeIntegers(t *testing.T) {
	const id = "9007199254740993"
	var p capability.Params
	require.NoError(t, json.Unmarshal([]byte(`{"body":{"id":`+id+`,"z":1}}`), &p))
	body, ok := p["body"].(map[string]any)
	require.True(t, ok)
	n, ok := body["id"].(json.Number)
	require.True(t, ok)
	assert.Equal(t, id, n.String())
}

func TestJSONSessionSendsLargeIntegerToExecutor(t *testing.T) {
	const id = "9007199254740993"
	exec := &captureExec{}
	srv := testServer(t, exec)
	line := `{"invoke":{"operation_id":"orders.get","params":{"id":` + id + `}}}` + "\n"
	require.NoError(t, capability.RunJSON(context.Background(), srv, strings.NewReader(line), io.Discard))
	require.Equal(t, id, exec.params["id"])
}

func TestJSONSessionKeepsSentOnLostResponse(t *testing.T) {
	exec := &captureExec{sent: true, err: errors.New("lost response")}
	srv := testServer(t, exec)
	var out bytes.Buffer
	err := capability.RunJSON(context.Background(), srv, strings.NewReader(`{"invoke":{"operation_id":"orders.get","params":{"id":"1"}}}`+"\n"), &out)
	require.Error(t, err)
	var res capability.InvokeResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &res))
	assert.True(t, res.Sent)
	assert.Equal(t, "lost response", res.Error)
}

func TestApproveAndResubmitNestedBodyIsStable(t *testing.T) {
	exec := &captureExec{}
	srv := testServer(t, exec)
	ctx := context.Background()
	for range 32 {
		first, err := srv.Call(ctx, runtime.Request{
			Operation: "orders.retire",
			Arguments: map[string]any{"body": map[string]any{"z": json.Number("1"), "a": "two", "nested": map[string]any{"k": "v", "b": "w"}}},
		})
		require.NoError(t, err)
		require.Equal(t, "confirmation_required", first.Status)
		approved, err := srv.Calls.State.Approve(ctx, first.ApprovalID)
		require.NoError(t, err)
		second, err := srv.Call(ctx, runtime.Request{
			Operation: "orders.retire",
			Arguments: map[string]any{"body": map[string]any{"a": "two", "nested": map[string]any{"b": "w", "k": "v"}, "z": json.Number("1")}},
			Approval:  approved,
		})
		require.NoError(t, err)
		assert.Equal(t, "ok", second.Status)
	}
}

func TestJSONSessionStopsWhenReaderCloses(t *testing.T) {
	r, w := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- capability.RunJSON(ctx, &capability.Server{}, r, io.Discard)
	}()
	_, err := io.WriteString(w, `{"search":{"query":"x"}}`+"\n")
	require.NoError(t, err)
	cancel()
	require.NoError(t, r.Close())
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunJSON stayed blocked after the reader closed")
	}
}
