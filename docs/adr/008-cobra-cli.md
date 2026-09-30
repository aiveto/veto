# ADR 008: Cobra for the veto binary only

## Context

The CLI exposes `validate`, `serve`, `eval`, `generate`, and `replay`. ADR 008 first chose `github.com/alecthomas/kong`.

## Staff engineer

Cobra is the familiar default in Go cloud and platform tooling. The veto binary is small, but contributors and enterprise users recognize Cobra. Kong gave no capability we cannot get from Cobra at this size.

## Architect

Kong kept one struct tree with tags. Cobra adds a command tree and more boilerplate. That trade is worth the ecosystem fit for an open source operator binary.

## Decision

Use `github.com/spf13/cobra` only in `cmd/veto`. Generated per-API CLIs stay on the standard library `flag` package unless a later change says otherwise.

## What we refused

Pulling cobra into core packages. Keeping kong for familiarity with one author only.
