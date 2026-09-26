# ADR 015: Replay reads OpenTelemetry spans

## Context

Stdout traces exist. There is no way to read a run back: model input, the tools that were visible, the policy decision, and the approval.

## Staff engineer

Keep the spans that already exist. An in-memory exporter records them. Replay prints those spans in start order. User messages and parameter values are omitted unless retention is turned on.

## Architect

A second JSON trace format would be easier to snapshot in tests.

## Decision

`veto replay` runs one message and prints the spans. `replay_redact` defaults to on. While it is on, replay keeps `operation.id`, `decision`, `http.method`, `http.status`, `approval.id`, `flow.name`, and `tools`. Every other attribute is omitted. `--keep-sensitive` prints all of them. ADR 007 still holds: no bespoke trace schema.

## What we refused

A custom trace file format. Metrics. A second model call during replay.
