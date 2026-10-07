package agent

import (
	"context"
	"fmt"

	"github.com/aiveto/veto/runtime"
)

const maxFollowCalls = 8

// Follow runs operationID through Invoke, then each declared relation or link that names a value.
// A link with no parameter mapping is not called. A missing response field is an error.
func (l *Loop) Follow(ctx context.Context, operationID string, params map[string]string, approvalID string) ([]Call, error) {
	first, err := l.Invoke(ctx, operationID, params, approvalID)
	if err != nil || first.Status != "ok" {
		return []Call{first}, err
	}
	calls := []Call{first}
	if l.Catalog == nil {
		return calls, nil
	}
	for _, link := range l.Catalog.Links {
		if link.From != operationID {
			continue
		}
		nextParams, ok, err := runtime.LinkParams(link, first.Body, l.Catalog.ByID(link.To))
		if err != nil {
			return calls, err
		}
		if !ok {
			continue
		}
		if len(calls) >= maxFollowCalls {
			return calls, fmt.Errorf("follow stopped after %d calls", maxFollowCalls)
		}
		next, err := l.Invoke(ctx, link.To, nextParams, "")
		calls = append(calls, next)
		if err != nil || next.Status != "ok" {
			return calls, err
		}
	}
	return calls, nil
}
