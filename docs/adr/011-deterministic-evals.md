# ADR 011: Evals use scripted model and real policy

## Context

We need a regression that destructive delete stops before HTTP and names the right operation.

## Staff engineer

Evals must not call a network LLM. Use the scripted model plus the same policy and invoke stack as `serve`.

## Architect

Model-graded evals catch wording drift but are flaky in CI.

## Decision

`veto eval` runs yaml cases against `model.Scripted` and `policy.Builtin`. Shipped case: "delete asset 123" requires confirmation and targets `assets.delete`.

## What we refused

Live model providers in the default eval path.
