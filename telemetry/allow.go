package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type allowExporter struct {
	next sdktrace.SpanExporter
}

func (e allowExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	out := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, sp := range spans {
		out[i] = allowSpan{ReadOnlySpan: sp}
	}
	return e.next.ExportSpans(ctx, out)
}

func (e allowExporter) Shutdown(ctx context.Context) error {
	return e.next.Shutdown(ctx)
}

type allowSpan struct {
	sdktrace.ReadOnlySpan
}

func (s allowSpan) Attributes() []attribute.KeyValue {
	raw := s.ReadOnlySpan.Attributes()
	out := make([]attribute.KeyValue, 0, len(raw))
	for _, kv := range raw {
		if Allowed(string(kv.Key)) {
			out = append(out, kv)
		}
	}
	return out
}
