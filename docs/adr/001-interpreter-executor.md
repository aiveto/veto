# ADR 001: Interpreter executor before codegen

## Context

The catalog must drive SDK, CLI, and MCP. We need a way to call HTTP from operation metadata on day one.

## Staff engineer

`veto serve` must work right after `validate` with no generate step. Teams should try MCP against a real spec in one command.

## Architect

Codegen first gives typed clients and catches schema mistakes at compile time. The executor duplicates work the generator would remove.

## Decision

Ship an interpreter that executes operations from the catalog via `net/http`. Typed SDK codegen is the next slice.

## What we refused

Making `go generate` or a codegen pass mandatory before `serve` or `eval`.
