# ADR 007: OpenTelemetry for traces

Instrument with `go.opentelemetry.io/otel`. Spans cover the model request, the policy decision, invoke, and confirmation. Local runs need no exporter.

Refused: a bespoke trace schema.
