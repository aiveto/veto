package model

import (
	"context"
	"regexp"
	"strings"

	"github.com/aiveto/veto/telemetry"
)

type (
	// Request is input to the model provider.
	Request struct {
		UserMessage string
		Context     string
	}

	// Response is the model's chosen operation and parameters.
	Response struct {
		OperationID string
		Params      map[string]string
		FlowName    string
	}

	// Model selects operations from user text.
	Model interface {
		Complete(ctx context.Context, req Request) (Response, error)
	}

	// Scripted maps phrases to operations for evals and tests.
	Scripted struct {
		patterns []scriptPattern
	}

	scriptPattern struct {
		re          *regexp.Regexp
		operationID string
		paramNames  []string
	}
)

// NewScripted builds the default scripted model for asset delete evals.
func NewScripted() *Scripted {
	return &Scripted{
		patterns: []scriptPattern{
			{
				re:          regexp.MustCompile(`(?i)delete\s+asset\s+(\d+)`),
				operationID: "assets.delete",
				paramNames:  []string{"id"},
			},
			{
				re:          regexp.MustCompile(`(?i)delete\s+asset\s+(\w+)`),
				operationID: "assets.delete",
				paramNames:  []string{"id"},
			},
		},
	}
}

// WithOperation rewrites the default delete pattern to use operationID.
func (s *Scripted) WithOperation(operationID string) *Scripted {
	out := *s
	for i := range out.patterns {
		if strings.Contains(out.patterns[i].operationID, "delete") || out.patterns[i].operationID == "deleteAsset" {
			out.patterns[i].operationID = operationID
		}
	}
	return &out
}

func (s *Scripted) Complete(ctx context.Context, req Request) (Response, error) {
	span := telemetry.StartSpan(ctx, "model.request")
	defer span.End()
	msg := strings.TrimSpace(req.UserMessage)
	if msg != "" {
		span.SetAttributes(telemetry.Attr("user_message", msg))
	}
	if msg != "" && !strings.Contains(req.Context, msg) {
		return Response{}, nil
	}
	for _, p := range s.patterns {
		m := p.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		params := map[string]string{}
		for i, name := range p.paramNames {
			if i+1 < len(m) {
				params[name] = m[i+1]
			}
		}
		span.SetAttributes(telemetry.Attr("operation.id", p.operationID))
		return Response{OperationID: p.operationID, Params: params}, nil
	}
	return Response{}, nil
}
