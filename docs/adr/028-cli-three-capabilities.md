# ADR 028: One capability kernel, MCP and JSON as adapters

`capability` holds search, describe, invoke, and Server. MCP and `veto serve --json` call Server. The JSON session loads the catalog once and reads those three as lines. `veto search`, `describe`, and `invoke` are a human shorthand. `--help-json` is the capability list. The generated per-API CLI stays typed invoke.

Refused: a second search path. A second help schema. The CLI importing the contract from `mcpserver`. Server living in `mcpserver`.
