# ADR 003: Default MCP is three capabilities plus pins

## Context

Large OpenAPI specs can define hundreds of operations. MCP clients suffer when every operation becomes its own tool.

## Staff engineer

Default to `capabilities_search`, `capabilities_describe`, and `capabilities_invoke`. Pin a few hot operations when needed; do not explode the tool list.

## Architect

Direct one-tool-per-operation is simpler to implement and matches many OpenAPI→MCP generators.

## Decision

Register only the three capability tools by default. Repeatable `--pin` lists pinned operation ids. `--direct-pins` may add direct tools for those ids only. Never auto-register every operation.

## What we refused

Blind 1:1 exposure of all operations as MCP tools.
