package runtime

import (
	"context"

	"github.com/aiveto/veto/catalog"
)

type (
	// Drafter is an Executor that can show Preview the request, before credentials, with secret values removed.
	Drafter interface {
		Draft(ctx context.Context, op *catalog.Operation, params map[string]string) (HTTPRequest, error)
	}

	// Projection names the response fields and the page an invoke asks for.
	// An empty Fields list leaves the body unchanged.
	Projection struct {
		Fields []string
		Offset int
		Limit  int
	}

	idempotencyKey struct{}
	projectionKey  struct{}
)

// WithIdempotency carries the invoke's idempotency key to the Executor.
func WithIdempotency(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyKey{}, key)
}

// IdempotencyFrom is the key set by WithIdempotency, or empty.
func IdempotencyFrom(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKey{}).(string)
	return key
}

// WithProjection carries the invoke's fields and page to the Executor. They override the Executor's own list.
func WithProjection(ctx context.Context, p Projection) context.Context {
	return context.WithValue(ctx, projectionKey{}, p)
}

// ProjectionFrom reports a projection set by WithProjection.
func ProjectionFrom(ctx context.Context) (Projection, bool) {
	p, ok := ctx.Value(projectionKey{}).(Projection)
	return p, ok
}
