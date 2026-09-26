# ADR 008: Kong for the veto binary only

## Context

The CLI exposes `validate`, `serve`, and `eval`.

## Staff engineer

Kong keeps struct-tagged commands small for a single binary. Generated per-API CLIs can use another library later.

## Architect

Cobra is familiar for large command trees; we may generate cobra for API CLIs.

## Decision

Use `github.com/alecthomas/kong` only in `cmd/veto`.

## What we refused

Pulling cobra into the core framework module for this binary.
