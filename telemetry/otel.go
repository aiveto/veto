package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/aiveto/veto"

// StartSpan begins a child span on the global tracer.
func StartSpan(ctx context.Context, name string) trace.Span {
	_, span := otel.Tracer(tracerName).Start(ctx, name)
	return span
}

// Attr builds one string attribute.
func Attr(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

// Install sets the process tracer. An empty export keeps the default noop provider.
// "stdout" writes spans to standard output. The returned function flushes and shuts the provider down.
func Install(export string) (func(context.Context) error, error) {
	if export == "" {
		return func(context.Context) error { return nil }, nil
	}
	if export != "stdout" {
		return nil, fmt.Errorf("trace export %q is not in this slice", export)
	}
	exp, err := stdouttrace.New()
	if err != nil {
		return nil, fmt.Errorf("stdout trace: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Span is one finished span with string attributes.
type Span struct {
	Name  string
	Start time.Time
	Attrs map[string]string
}

// Recorder keeps finished spans in memory so replay can read them.
type Recorder struct {
	exp  *tracetest.InMemoryExporter
	stop func(context.Context) error
	prev trace.TracerProvider
}

// Record installs an in-memory tracer. Stop restores the previous provider.
// Read Spans before Stop. Shutdown clears the exporter.
func Record() (*Recorder, error) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	return &Recorder{exp: exp, prev: prev, stop: tp.Shutdown}, nil
}

// Spans returns finished spans. Call this before Stop.
func (r *Recorder) Spans() []Span {
	if r == nil || r.exp == nil {
		return nil
	}
	stubs := r.exp.GetSpans()
	out := make([]Span, 0, len(stubs))
	for _, s := range stubs {
		attrs := map[string]string{}
		for _, kv := range s.Attributes {
			if kv.Value.Type() == attribute.STRING {
				attrs[string(kv.Key)] = kv.Value.AsString()
			}
		}
		out = append(out, Span{Name: s.Name, Start: s.StartTime, Attrs: attrs})
	}
	return out
}

// Stop flushes the provider and restores the previous tracer.
func (r *Recorder) Stop(ctx context.Context) error {
	if r == nil || r.stop == nil {
		return nil
	}
	err := r.stop(ctx)
	if r.prev != nil {
		otel.SetTracerProvider(r.prev)
	}
	r.stop = nil
	return err
}
