package capability

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/jsonopts"
	"github.com/aiveto/veto/result"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/semantics"
)

type (
	// SearchHit is one search result on the wire. Describe still returns the operation.
	SearchHit struct {
		ID           string   `json:"id"`
		Call         string   `json:"call"`
		Related      []string `json:"related,omitempty"`
		Confirmation bool     `json:"confirmation,omitempty"`
	}

	InvokeResult struct {
		Status      string       `json:"status"`
		ApprovalID  string       `json:"approval_id,omitempty"`
		OperationID string       `json:"operation_id,omitempty"`
		HTTPStatus  int          `json:"http_status,omitempty"`
		Body        string       `json:"body,omitempty"`
		Code        string       `json:"code,omitempty"`
		Retryable   bool         `json:"retryable"`
		RetryAfter  string       `json:"retry_after,omitempty"`
		Error       string       `json:"error,omitempty"`
		Truncated   bool         `json:"truncated,omitempty"`
		Page        *result.Page `json:"page,omitempty"`
		Why         string       `json:"why,omitempty"`
		Caller      string       `json:"caller,omitempty"`
		HTTP        bool         `json:"http"`
		Sent        bool         `json:"sent,omitempty"`
	}

	Server struct {
		Catalog   *catalog.Catalog
		Semantics semantics.Notes
		Calls     *runtime.Runtime
		synOnce   sync.Once
		syns      map[string][]string
	}
)

func (s *Server) searchSyns() map[string][]string {
	if s == nil {
		return nil
	}
	s.synOnce.Do(func() {
		if s.Semantics != nil {
			s.syns = s.Semantics.AllSynonyms()
		}
	})
	return s.syns
}

func (s *Server) Search(query string, offset, limit int) []SearchHit {
	var cat *catalog.Catalog
	if s != nil {
		cat = s.Catalog
	}
	syns := s.searchSyns()
	matches := catalog.SearchPage(cat, query, syns, offset, limit)
	if len(matches) == 0 {
		return nil
	}
	out := make([]SearchHit, 0, len(matches))
	for _, m := range matches {
		note := ""
		if s != nil && s.Semantics != nil {
			note = s.Semantics.Note(m.Operation.ID).Text()
		}
		out = append(out, SearchHit{
			ID:           m.Operation.ID,
			Call:         runctx.OperationLine(cat, m.Operation, note),
			Related:      m.Related,
			Confirmation: m.Operation.RequiresConfirmation,
		})
	}
	return out
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
	return s.Encode(payload)
}

func (s *Server) RunSearch(args SearchArgs) ([]byte, error) {
	return s.Encode(s.Search(args.Query, args.Offset, args.Limit))
}

func (s *Server) RunDescribe(args DescribeArgs) ([]byte, error) {
	return s.Describe(args.OperationID)
}

// Request is the runtime call for these arguments. Caller is set by the adapter.
func (a InvokeArgs) Request() runtime.Request {
	return runtime.Request{
		Operation: a.OperationID,
		Arguments: map[string]any(a.Params),
		Approval:  a.ApprovalID,
		Fields:    a.Fields,
		Offset:    a.Offset,
		Limit:     a.Limit,
	}
}

// RunInvoke encodes one invoke. MCP wraps IsError and elicitation around EncodeInvoke.
func (s *Server) RunInvoke(ctx context.Context, args InvokeArgs) ([]byte, error) {
	ctx = auth.WithUserToken(ctx, args.Token)
	req := args.Request()
	if args.Preview {
		_, b, err := s.EncodePreview(ctx, req)
		return b, err
	}
	_, b, err := s.EncodeInvoke(ctx, req)
	return b, err
}

// EncodeInvoke runs Call, redacts, and encodes. Adapters share this body.
func (s *Server) EncodeInvoke(ctx context.Context, req runtime.Request) (InvokeResult, []byte, error) {
	res, err := s.Call(ctx, req)
	return s.EncodeResult(res, err)
}

// EncodePreview runs Preview and encodes. A failed preview still returns the body.
func (s *Server) EncodePreview(ctx context.Context, req runtime.Request) (runtime.Preview, []byte, error) {
	out, err := s.Preview(ctx, req)
	if err != nil {
		return out, nil, err
	}
	b, encErr := s.Encode(out)
	if encErr != nil {
		return out, b, encErr
	}
	if len(out.Errors) > 0 {
		return out, b, errors.New("preview failed")
	}
	return out, b, nil
}

// EncodeResult redacts and encodes one invoke result.
func (s *Server) EncodeResult(res InvokeResult, callErr error) (InvokeResult, []byte, error) {
	normalize(&res, callErr)
	b, encErr := s.Encode(res)
	if encErr != nil {
		return res, b, encErr
	}
	return res, b, callErr
}

func normalize(res *InvokeResult, callErr error) {
	if res == nil {
		return
	}
	if res.Status != runtime.StatusError {
		return
	}
	cause := res.Error
	if cause == "" && callErr != nil {
		cause = callErr.Error()
	}
	res.Error = Sanitize(cause)
}

// Encode writes the same JSON MCP returns for search, describe, and invoke.
func (s *Server) Encode(v any) ([]byte, error) {
	if s != nil && s.Calls != nil {
		return s.Calls.JSON.Marshal(v)
	}
	return (jsonopts.Set{}).Marshal(v)
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
		return InvokeResult{Status: runtime.StatusError, Error: "runtime required"}, errors.New("runtime required")
	}
	call, err := s.Calls.Invoke(ctx, req)
	return InvokeResult(call), err
}

// Handle runs exactly one of search, describe, or invoke on a line.
func (s *Server) Handle(ctx context.Context, line Line) ([]byte, error) {
	n := 0
	if line.Search != nil {
		n++
	}
	if line.Describe != nil {
		n++
	}
	if line.Invoke != nil {
		n++
	}
	if n != 1 {
		return nil, errors.New("exactly one of search, describe, invoke")
	}
	switch {
	case line.Search != nil:
		return s.RunSearch(*line.Search)
	case line.Describe != nil:
		return s.RunDescribe(*line.Describe)
	default:
		ctx = auth.WithCaller(ctx, auth.OrLocal(line.Caller))
		return s.RunInvoke(ctx, *line.Invoke)
	}
}
