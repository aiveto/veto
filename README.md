# veto

Contract-driven agent surface for Go. Register one or more OpenAPI files into one **catalog**. A relations file joins a schema field to an operation in another API. MCP stays three tools (`capabilities_search`, `capabilities_describe`, `capabilities_invoke`). The context pack walks those edges and does not embed the raw spec. Policy still stops a destructive call until confirmation is stored. Each contract keeps its own server URL.

Module: `github.com/aiveto/veto`

## Commands

```bash
go run ./cmd/veto validate --config examples/two-apis/veto.yaml
go run ./cmd/veto validate --config testdata/veto.yaml
go run ./cmd/veto serve --config testdata/veto.yaml --stdio
go run ./cmd/veto eval --config testdata/veto.yaml --case testdata/cases
go run ./cmd/veto pack --config testdata/veto.yaml --message "delete order 123"
go run ./cmd/veto doctor --config testdata/veto.yaml
go run ./cmd/veto check --config testdata/veto.yaml --case testdata/cases
go run ./cmd/veto generate --config testdata/veto.yaml --out generated --module example.com/orders
go run ./cmd/veto replay --config testdata/veto.yaml --message "delete order 123"
```

`testdata/veto.yaml` lists the contracts, the relations file, semantics, agent metadata, and the provider keys. Repeat `--contract` only to override that list. Eval cases live in `testdata/cases`. `--case` takes a file or a directory, and it repeats. `veto pack` prints the same pack `serve` builds. `policy: opa` and `policy: spicedb` are accepted keys and fail closed. `execution: temporal` and `decision: jev` are accepted keys and fail closed. The clients are not imported. Unset, execution stays in-process and confirmation stays in veto. Unset policy stays builtin. Confirmation stays in this process: send `approval_id` back to resume. `VETO_APPROVAL_SECRET` makes that id an HMAC of the operation, the params, and an expiry. The caller holds the token and the process stores nothing. Unset, the pending call stays in the process. `veto check --against` a git ref or a snapshot file fails when a joined operation disappears, a destructive call loses confirmation without an agent.yaml change, or an eval case changes its operation or confirmation. When a contract lists more than one server, the first URL is used unless `server` names another. `page: follow` collects list pages up to five; otherwise a list is one request. The context pack keeps the search hits and their neighbors. `model: openai` reads `OPENAI_API_KEY` and that pack. The default model is `scripted`. `memory: file` with `memory_file` is an optional turn log. Unset memory stays in the process. `examples/two-apis` is the orders catalog plus customers. `serve --grouped` adds one tool per resource. Replay runs the message and prints the trace. It omits the user message unless `--keep-sensitive` is set. `trace_file` writes that redacted view, and `replay --from` prints the file without running again. `trace_export: otlp` sends only the attributes replay keeps. Empty export stays a noop.

Work left before a public release is in `docs/before-open-source.md`.

## Layout

- `catalog/`: operation model and capability graph (resources, schemas, OpenAPI links)
- `agentmeta/`: `agent.yaml` overlay (confirmation, permissions, exposure)
- `replay/`: read OpenTelemetry spans for one run
- `openapi/`: kin-openapi loader
- `semantics/`: derived synonyms and yaml overlay
- `runctx/`: context pack builder (never embeds the raw spec)
- `policy/`: allow, check, confirmation state
- `agent/`: one turn: model, policy, execute. The follow-up pack goes to the caller
- `config/`: provider keys (`testdata/veto.yaml`)
- `mcpserver/`: MCP stdio server
- `execute/`: HTTP invoke from catalog operations
- `eval/`: deterministic eval cases (scripted model, real policy path)
- `generate/`: typed SDK and CLI (`--help-json`). Calls go through `agent.Invoke`

See `docs/adr/` for design decisions.

## Test

```bash
go test ./...
```
