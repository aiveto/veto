# ADR 018: The shape stands

The loop accepts an `Executor`. `execute.Client` is the HTTP writer and the only place that builds a request. Confirmation state is locked and keeps its own params. Marshal errors and body errors are returned. Operations are sorted by id.

Refused: a persistence layer. A second MCP tool per operation.
