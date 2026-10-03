# ADR 023: Drop keys that accept one value

## Context

`veto.yaml` had `decision`, `execution`, `subagents`, and `telemetry`. Each accepted one value and failed on any other. Each was a field, a default, and a check in two places. `replay_redact` was a string that had to read `true` or `false`. `policy.State` had `RequestConfirmation` and `ConsumeConfirmation`, which only passed an empty caller on, and `execute.ReadBody` had no caller.

## Staff engineer

A key with one legal value is a promise of a provider that does not exist. Signed decision 7 names the defaults for memory, model, semantics, and policy. These four are not on that list. Strict field checking already names an unknown key, so a file that still sets one fails with the key in the message. `replay_redact` becomes `*bool`, like `confirmation`. Unset still redacts.

## Architect

When a second provider exists, its key comes back with that provider. The test-only wrappers go, and the tests call `RequestFor` and `ConsumeFor` with an empty caller.

## Decision

Remove `decision`, `execution`, `subagents`, and `telemetry` from `veto.yaml`. `replay_redact` is a bool. Remove `RequestConfirmation`, `ConsumeConfirmation`, and `execute.ReadBody`.

## What we refused

Accepting and ignoring the old keys.
