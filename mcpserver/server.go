package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
)

type (
	InvokeResult struct {
		Status      string `json:"status"`
		ApprovalID  string `json:"approval_id,omitempty"`
		OperationID string `json:"operation_id,omitempty"`
		HTTPStatus  int    `json:"http_status,omitempty"`
		Body        string `json:"body,omitempty"`
		Code        string `json:"code,omitempty"`
		Retryable   bool   `json:"retryable"`
		Error       string `json:"error,omitempty"`
	}

	Server struct {
		Catalog   *catalog.Catalog
		Semantics agent.Notes
		Agent     *agent.Loop
	}
)

func (s *Server) Search(ctx context.Context, query string, offset, limit int) []catalog.Match {
	var syns map[string][]string
	if s.Semantics != nil {
		syns = s.Semantics.AllSynonyms()
	}
	return catalog.SearchPage(s.Catalog, query, syns, offset, limit)
}

func (s *Server) Describe(ctx context.Context, operationID string) ([]byte, error) {
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

func (s *Server) Invoke(ctx context.Context, operationID string, params map[string]string, approvalID string) (InvokeResult, error) {
	if s.Agent == nil {
		return InvokeResult{Status: "error", Error: "agent required"}, fmt.Errorf("agent required")
	}
	call, err := s.Agent.Invoke(ctx, operationID, params, approvalID)
	return InvokeResult{
		Status:      call.Status,
		ApprovalID:  call.ApprovalID,
		OperationID: call.OperationID,
		HTTPStatus:  call.HTTPStatus,
		Body:        call.Body,
		Code:        call.Code,
		Retryable:   call.Retryable,
		Error:       call.Error,
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
		for _, p := range pins {
			names = append(names, p)
		}
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
