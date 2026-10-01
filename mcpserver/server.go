package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/semantics"
)

type (
	InvokeResult struct {
		Status      string       `json:"status"`
		ApprovalID  string       `json:"approval_id,omitempty"`
		OperationID string       `json:"operation_id,omitempty"`
		HTTPStatus  int          `json:"http_status,omitempty"`
		Body        string       `json:"body,omitempty"`
		Code        string       `json:"code,omitempty"`
		Retryable   bool         `json:"retryable"`
		Error       string       `json:"error,omitempty"`
		Truncated   bool         `json:"truncated,omitempty"`
		Page        *result.Page `json:"page,omitempty"`
	}

	Server struct {
		Catalog   *catalog.Catalog
		Semantics agent.Notes
		Calls     *runtime.Runtime
	}
)

func (s *Server) Search(query string, offset, limit int) []catalog.Match {
	var syns map[string][]string
	if s.Semantics != nil {
		syns = s.Semantics.AllSynonyms()
	}
	return catalog.SearchPage(s.Catalog, query, syns, offset, limit)
}

func (s *Server) Describe(operationID string) ([]byte, error) {
	op := s.Catalog.ByID(operationID)
	if op == nil {
		return nil, fmt.Errorf("unknown operation %q", operationID)
	}
	var note semantics.Note
	if s.Semantics != nil {
		note = s.Semantics.Note(operationID)
	}
	payload := map[string]any{
		"operation": *op,
		"semantics": note,
		"related":   s.Catalog.Graph.Related(operationID),
		"schemas":   s.Catalog.Graph.Schemas(operationID),
		"call":      runctx.OperationLine(s.Catalog, *op, note.Text()),
		"relation":  note.Relation,
	}
	return json.Marshal(payload)
}

// Invoke adapts string parameters at the MCP boundary and calls the shared runtime.
func (s *Server) Invoke(ctx context.Context, operationID string, params map[string]string, approvalID string) (InvokeResult, error) {
	return s.Call(ctx, runtime.Request{
		Operation: operationID,
		Arguments: runtime.FromStrings(params),
		Approval:  approvalID,
	})
}

// Preview runs the shared runtime and stops before a token URL and upstream HTTP.
func (s *Server) Preview(ctx context.Context, req runtime.Request) (runtime.Preview, error) {
	if s == nil || s.Calls == nil {
		return runtime.Preview{}, errors.New("runtime required")
	}
	return s.Calls.Preview(ctx, req)
}

// Call runs one typed invoke. It does not use the agent loop.
func (s *Server) Call(ctx context.Context, req runtime.Request) (InvokeResult, error) {
	if s == nil || s.Calls == nil {
		return InvokeResult{Status: "error", Error: "runtime required"}, errors.New("runtime required")
	}
	call, err := s.Calls.Invoke(ctx, req)
	return InvokeResult{
		Status:      call.Status,
		ApprovalID:  call.ApprovalID,
		OperationID: call.OperationID,
		HTTPStatus:  call.HTTPStatus,
		Body:        call.Body,
		Code:        call.Code,
		Retryable:   call.Retryable,
		Error:       call.Error,
		Truncated:   call.Truncated,
		Page:        call.Page,
	}, err
}

// Grouped mode adds one tool per resource, never one tool per operation.
func ToolNames(cat *catalog.Catalog, pins []string, directPins, grouped bool) []string {
	names := []string{
		"capabilities_search",
		"capabilities_describe",
		"capabilities_invoke",
	}
	if directPins {
		names = append(names, pins...)
	}
	if grouped {
		names = append(names, groupedResources(cat)...)
	}
	return names
}

func groupedResources(cat *catalog.Catalog) []string {
	if cat == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, op := range cat.Operations {
		if op.Group == "" || op.Exposure == catalog.ExposureDiscovery || seen[op.Group] {
			continue
		}
		seen[op.Group] = true
		out = append(out, op.Group)
	}
	return out
}
