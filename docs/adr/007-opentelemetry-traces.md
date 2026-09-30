# ADR 007: OpenTelemetry for traces

## Context

Operators need to see where model requests, policy, and HTTP invoke time is spent.

## Staff engineer

Use the standard Go OpenTelemetry API. No custom trace JSON format to maintain.

## Architect

A lightweight hand-rolled trace log would be fewer dependencies for a demo.

## Decision

Instrument with `go.opentelemetry.io/otel`. Spans include model request, policy decision, invoke, and confirmation paths. Tests may rely on the default noop provider.

## What we refused

A bespoke trace schema or mandatory exporter configuration for local runs.
