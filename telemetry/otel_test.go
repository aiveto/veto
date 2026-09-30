package telemetry

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOTLPExportDropsUnlistedAttributes(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(allowExporter{next: mem}))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	_, span := tp.Tracer("t").Start(context.Background(), "agent.run")
	span.SetAttributes(
		attribute.String("operation.id", "orders.delete"),
		attribute.String("user_message", "Delete order 123"),
	)
	span.End()
	stubs := mem.GetSpans()
	require.NoError(t, tp.Shutdown(context.Background()))
	require.Len(t, stubs, 1)
	var keys []string
	for _, kv := range stubs[0].Attributes {
		keys = append(keys, string(kv.Key)+"="+kv.Value.AsString())
	}
	got := strings.Join(keys, " ")
	assert.Contains(t, got, "operation.id=orders.delete")
	assert.NotContains(t, got, "Delete order 123")
}
