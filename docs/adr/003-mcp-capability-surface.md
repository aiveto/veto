# ADR 003: Default MCP is three capabilities plus pins

MCP registers `capabilities_search`, `capabilities_describe`, and `capabilities_invoke`. `--pin` lists extra operation ids. `--direct-pins` may add tools for those ids only.

Refused: one MCP tool per operation.
