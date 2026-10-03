# ADR 001: Interpreter executor before codegen

`veto serve` executes from the catalog with `net/http`. `generate` writes an optional Go client that calls `runtime.Invoke`. Serving does not require codegen.

Refused: a generate step before `serve` or `eval`.
