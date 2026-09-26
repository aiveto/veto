# ADR 014: Graph links, schemas, and opt-in grouped tools

## Context

The graph is only resource to operation from the path noun. OpenAPI links and component schemas are dropped, so describe cannot name the next operation. Grouped exposure was an extension point, not a tool list.

## Staff engineer

Add schema nodes and link edges from the spec. Search hits a schema name. Describe returns related operations. Grouped mode registers one tool per resource, and discovery-only operations stay out of that tool and out of direct pins.

## Architect

A fourth MCP tool for the graph, or one tool per operation inside a group, is a smaller change to the client.

## Decision

Default MCP stays search, describe, and invoke. `--grouped` adds one tool per resource that still has a visible operation. An `operationRef` is a JSON pointer to a path operation in this document. An unresolved link fails the load. ADR 003 is unchanged for the default.

## What we refused

One MCP tool per operation. A code-mode sandbox.
