# ADR 014: Graph links, schemas, and opt-in grouped tools

Search hits a schema name. Describe returns related operations from OpenAPI links. `--grouped` adds one tool per resource that still has a visible operation. An unresolved `operationRef` fails the load. ADR 003 stays the default.

Refused: one MCP tool per operation. A code-mode sandbox.
