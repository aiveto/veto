package agent

import (
	"context"
	"regexp"
	"strings"

	"github.com/aiveto/veto/telemetry"
)

type (
	Scripted struct {
		patterns []scriptPattern
	}

	scriptPattern struct {
		re          *regexp.Regexp
		operationID string
		paramNames  []string
	}
)

func NewScripted() *Scripted {
	return &Scripted{
		patterns: []scriptPattern{
			{
				re:          regexp.MustCompile(`(?i)delete\s+order\s+(\w+)`),
				operationID: "orders.delete",
				paramNames:  []string{"id"},
			},
		},
	}
}

func (s *Scripted) WithOperation(operationID string) *Scripted {
	if s == nil {
		return &Scripted{}
	}
	out := &Scripted{patterns: make([]scriptPattern, len(s.patterns))}
	copy(out.patterns, s.patterns)
	for i := range out.patterns {
		if strings.Contains(out.patterns[i].operationID, "delete") || out.patterns[i].operationID == "deleteOrder" {
			out.patterns[i].operationID = operationID
		}
	}
	return out
}

func (s *Scripted) Complete(ctx context.Context, req Request) (Response, error) {
	_, span := telemetry.StartSpan(ctx, "model.request")
	defer span.End()
	if err := emptyPack(req); err != nil {
		return Response{}, err
	}
	msg := strings.TrimSpace(req.UserMessage)
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
