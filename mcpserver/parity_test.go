package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func parityServer(t *testing.T, exec runtime.Executor, allow map[string]bool) *capability.Server {
	t.Helper()
	cat := &catalog.Catalog{Operations: []catalog.Operation{
		{
			ID:           "orders.get",
			Method:       "GET",
			PathTemplate: "/orders/{id}",
			Permissions:  []string{"orders.read"},
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
	pol := policy.Hook(policy.Builtin{})
	if allow != nil {
		pol = policy.Builtin{Allow: allow}
	}
	rt := runtime.Runtime{
		Catalog: cat,
		Exec:    exec,
		State:   policy.NewState(),
		Policy:  pol,
		Base:    pol,
		Gate:    &runtime.InvokeGate{Per: 1024},
	}
	return &capability.Server{Catalog: cat, Calls: &rt}
}

type invokeOutcome struct {
	Status     string
	Code       string
	Sent       bool
	HTTP       bool
	Why        string
	Error      string
	Approval   bool
	PreviewErr bool
	Failed     bool
}

func TestMCPAndJSONShareInvokeOutcomes(t *testing.T) {
	const largeID = "9007199254740993"
	cases := []struct {
		name   string
		allow  map[string]bool
		exec   captureExec
		json   string
		mcp    map[string]any
		second map[string]any
		json2  string
		want   invokeOutcome
	}{
		{
			name: "success",
			json: `{"invoke":{"operation_id":"orders.get","params":{"id":"1"}}}`,
			mcp:  map[string]any{"operation_id": "orders.get", "params": map[string]any{"id": "1"}},
			want: invokeOutcome{Status: "ok", Code: "ok", Sent: true, HTTP: true},
		},
		{
			name:  "denial",
			allow: map[string]bool{},
			json:  `{"invoke":{"operation_id":"orders.get","params":{"id":"1"}}}`,
			mcp:   map[string]any{"operation_id": "orders.get", "params": map[string]any{"id": "1"}},
			want:  invokeOutcome{Status: "denied", Failed: true},
		},
		{
			name: "missing parameters",
			json: `{"invoke":{"operation_id":"orders.get"}}`,
			mcp:  map[string]any{"operation_id": "orders.get"},
			want: invokeOutcome{Status: "error", Code: "missing_param", Error: "operation orders.get: id required", Failed: true},
		},
		{
			name: "ambiguous execution failure",
			exec: captureExec{sent: true, err: errors.New("lost response")},
			json: `{"invoke":{"operation_id":"orders.get","params":{"id":"1"}}}`,
			mcp:  map[string]any{"operation_id": "orders.get", "params": map[string]any{"id": "1"}},
			want: invokeOutcome{Status: "error", Sent: true, Error: "lost response", Failed: true},
		},
		{
			name: "preview failure",
			json: `{"invoke":{"operation_id":"orders.get","preview":true}}`,
			mcp:  map[string]any{"operation_id": "orders.get", "preview": true},
			want: invokeOutcome{PreviewErr: true, Failed: true},
		},
		{
			name: "approved structured body",
			json: `{"invoke":{"operation_id":"orders.retire","params":{"body":{"z":1,"a":"two","nested":{"k":"v","b":"w"}}}}}`,
			mcp: map[string]any{
				"operation_id": "orders.retire",
				"params":       json.RawMessage(`{"body":{"z":1,"a":"two","nested":{"k":"v","b":"w"}}}`),
			},
			json2: `{"invoke":{"operation_id":"orders.retire","params":{"body":{"a":"two","nested":{"b":"w","k":"v"},"z":1}},"approval_id":%q}}`,
			second: map[string]any{
				"operation_id": "orders.retire",
				"params":       json.RawMessage(`{"body":{"a":"two","nested":{"b":"w","k":"v"},"z":1}}`),
			},
			want: invokeOutcome{Status: "ok", Code: "ok", Sent: true, HTTP: true},
		},
		{
			name: "large integer",
			json: `{"invoke":{"operation_id":"orders.get","params":{"id":` + largeID + `}}}`,
			mcp: map[string]any{
				"operation_id": "orders.get",
				"params":       json.RawMessage(`{"id":` + largeID + `}`),
			},
			want: invokeOutcome{Status: "ok", Code: "ok", Sent: true, HTTP: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jsonExec := tc.exec
			mcpExec := tc.exec
			jsonGot := runJSONCase(t, parityServer(t, &jsonExec, tc.allow), tc.json, tc.json2)
			mcpGot := runMCPCase(t, parityServer(t, &mcpExec, tc.allow), tc.mcp, tc.second)
			assert.Equal(t, jsonGot, mcpGot)
			assert.Equal(t, tc.want, jsonGot)
			if tc.name == "large integer" {
				assert.Equal(t, largeID, jsonExec.params["id"])
				assert.Equal(t, largeID, mcpExec.params["id"])
			}
		})
	}
}

func runJSONCase(t *testing.T, srv *capability.Server, first, secondFmt string) invokeOutcome {
	t.Helper()
	out, err := jsonLine(srv, first)
	if secondFmt == "" {
		return decodeOutcome(t, out, err)
	}
	require.NoError(t, err)
	var held capability.InvokeResult
	require.NoError(t, json.Unmarshal(out, &held))
	require.Equal(t, "confirmation_required", held.Status)
	approved, err := srv.Calls.State.Approve(context.Background(), held.ApprovalID)
	require.NoError(t, err)
	out, err = jsonLine(srv, fmt.Sprintf(secondFmt, approved))
	return decodeOutcome(t, out, err)
}

func runMCPCase(t *testing.T, srv *capability.Server, first, second map[string]any) invokeOutcome {
	t.Helper()
	session := openMCP(t, srv)
	res, text := callInvoke(t, session, first)
	if second == nil {
		return mcpOutcome(t, res, text)
	}
	require.False(t, res.IsError)
	var held capability.InvokeResult
	require.NoError(t, json.Unmarshal([]byte(text), &held))
	require.Equal(t, "confirmation_required", held.Status)
	approved, err := srv.Calls.State.Approve(context.Background(), held.ApprovalID)
	require.NoError(t, err)
	second["approval_id"] = approved
	res, text = callInvoke(t, session, second)
	return mcpOutcome(t, res, text)
}

func jsonLine(srv *capability.Server, line string) ([]byte, error) {
	var out bytes.Buffer
	err := capability.RunJSON(context.Background(), srv, strings.NewReader(line+"\n"), &out)
	return bytes.TrimSpace(out.Bytes()), err
}

func decodeOutcome(t *testing.T, raw []byte, err error) invokeOutcome {
	t.Helper()
	if previewFailed(raw) {
		return invokeOutcome{PreviewErr: true, Failed: err != nil}
	}
	var res capability.InvokeResult
	require.NoError(t, json.Unmarshal(raw, &res))
	return invokeOutcome{
		Status:   res.Status,
		Code:     res.Code,
		Sent:     res.Sent,
		HTTP:     res.HTTP,
		Why:      res.Why,
		Error:    res.Error,
		Approval: res.ApprovalID != "",
		Failed:   err != nil || res.Status == runtime.StatusDenied,
	}
}

func mcpOutcome(t *testing.T, res *mcp.CallToolResult, text string) invokeOutcome {
	t.Helper()
	if previewFailed([]byte(text)) {
		return invokeOutcome{PreviewErr: true, Failed: res.IsError}
	}
	var out capability.InvokeResult
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	return invokeOutcome{
		Status:   out.Status,
		Code:     out.Code,
		Sent:     out.Sent,
		HTTP:     out.HTTP,
		Why:      out.Why,
		Error:    out.Error,
		Approval: out.ApprovalID != "",
		Failed:   res.IsError,
	}
}

func previewFailed(raw []byte) bool {
	var preview runtime.Preview
	if json.Unmarshal(raw, &preview) != nil {
		return false
	}
	return len(preview.Errors) > 0
}

func openMCP(t *testing.T, srv *capability.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newMCP(srv, Options{}).Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "dev"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callInvoke(t *testing.T, session *mcp.ClientSession, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: capability.InvokeName, Arguments: args})
	require.NoError(t, err)
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return res, text.Text
}

func TestJSONMissingParamKeepsStructuredResult(t *testing.T) {
	var out bytes.Buffer
	err := capability.RunJSON(context.Background(), parityServer(t, &captureExec{}, nil), strings.NewReader(`{"invoke":{"operation_id":"orders.get"}}`+"\n"), &out)
	require.Error(t, err)
	var res capability.InvokeResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &res))
	assert.Equal(t, "missing_param", res.Code)
	assert.False(t, res.Sent)
}
