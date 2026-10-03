# ADR 023: Drop keys that accept one value

`veto.yaml` has no `decision`, `execution`, `subagents`, or `telemetry` keys. `replay_redact` is a bool. Unset still redacts.

Refused: accepting and ignoring the old keys.
