# ADR 015: Replay reads OpenTelemetry spans

`veto replay` prints the spans from one run. `replay_redact` defaults to on. While it is on, replay keeps `operation.id`, `decision`, `http.method`, `http.status`, `approval.id`, `flow.name`, and `tools`. `--keep-sensitive` records response bodies. Parameter values and the user message stay off the span. ADR 007 still holds.

Refused: a custom trace file format. Metrics. A second model call during replay.
