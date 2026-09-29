package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInstallEmptyKeepsNoop(t *testing.T) {
	stop, err := Install("")
	if err != nil {
		t.Fatal(err)
	}
	if err := stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	span := StartSpan(context.Background(), "agent.run")
	span.End()
}

func TestOTLPExportDropsUnlistedAttributes(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(allowExporter{next: mem}))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	_, span := tp.Tracer("t").Start(context.Background(), "agent.run")
	span.SetAttributes(
		attribute.String("operation.id", "assets.delete"),
		attribute.String("user_message", "Delete asset 123"),
	)
	span.End()
	stubs := mem.GetSpans()
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(stubs) != 1 {
		t.Fatalf("spans: %d", len(stubs))
	}
	var keys []string
	for _, kv := range stubs[0].Attributes {
		keys = append(keys, string(kv.Key)+"="+kv.Value.AsString())
	}
	got := strings.Join(keys, " ")
	if !strings.Contains(got, "operation.id=assets.delete") || strings.Contains(got, "Delete asset 123") {
		t.Fatalf("exported: %s", got)
	}
}
