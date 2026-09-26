# ADR 005: Official MCP Go SDK

## Context

`veto serve` must host stdio MCP tools in Go.

## Staff engineer

Prefer the official `github.com/modelcontextprotocol/go-sdk` for protocol compatibility and long-term maintenance.

## Architect

`mark3labs/mcp-go` is widely used and may have more examples today.

## Decision

Use `github.com/modelcontextprotocol/go-sdk` for the MCP server. It supports stdio tools via `mcp.StdioTransport` and `mcp.AddTool`.

## What we refused

Taking a hard dependency on `mcp-go` in this slice. Revisit only if the official SDK cannot host stdio tools (not the case here).
