package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName = "github.com/aiveto/veto"

	// GenAI attribute names. The genai module is not a dependency.
	ToolNameAttr    = "gen_ai.tool.name"
	OperationIDAttr = "gen_ai.operation.name"
)

type (
	Span struct {
		Name  string
		Start time.Time
		Attrs map[string]string
	}

	Recorder struct {
		exp  *tracetest.InMemoryExporter
		stop func(context.Context) error
		prev trace.TracerProvider
	}
)

func StartSpan(ctx context.Context, name string) trace.Span {
	_, span := otel.Tracer(tracerName).Start(ctx, name)
	return span
}

func Attr(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

func Allowed(key string) bool {
	switch key {
	case "operation.id", "decision", "http.method", "http.status", "approval.id", "flow.name", "tools", ToolNameAttr, OperationIDAttr:
		return true
	default:
		return false
	}
}

func Install(export string) (func(context.Context) error, error) {
	if export == "" {
		return func(context.Context) error { return nil }, nil
	}
	var exp sdktrace.SpanExporter
	var err error
	switch export {
	case "stdout":
		exp, err = stdouttrace.New()
	case "otlp":
		exp, err = otlptracehttp.New(context.Background())
		if err == nil {
			exp = allowExporter{next: exp}
		}
	default:
		return nil, fmt.Errorf("trace export %q is not in this slice", export)
	}
	if err != nil {
		return nil, fmt.Errorf("%s trace: %w", export, err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Read Spans before Stop. Shutdown clears the exporter.
func Record() (*Recorder, error) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	return &Recorder{exp: exp, prev: prev, stop: tp.Shutdown}, nil
}

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
