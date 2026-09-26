# veto

Contract-driven agent surface for Go. Register one or more OpenAPI files into one **catalog**. A relations file joins a schema field to an operation in another API. MCP stays three tools (`capabilities_search`, `capabilities_describe`, `capabilities_invoke`). The context pack walks those edges and does not embed the raw spec. Policy still stops a destructive call until confirmation is stored. Each contract keeps its own server URL.

Module: `github.com/aiveto/veto`

## Commands

```bash
go run ./cmd/veto validate --config testdata/veto.yaml
go run ./cmd/veto serve --config testdata/veto.yaml --stdio
go run ./cmd/veto eval --config testdata/veto.yaml --case testdata/delete.yaml
go run ./cmd/veto generate --config testdata/veto.yaml --out generated --module example.com/assets
go run ./cmd/veto replay --config testdata/veto.yaml --message "delete asset 123"
```

`testdata/veto.yaml` lists the contracts, the relations file, semantics, agent metadata, and the provider keys. Repeat `--contract` only to override that list. The context pack keeps the search hits and their neighbors. `model: openai` reads `OPENAI_API_KEY` and that pack. The default model is `scripted`. `serve --grouped` adds one tool per resource. Replay runs the message and prints the trace. It omits the user message unless `--keep-sensitive` is set.

Work left before a public release is in `docs/before-open-source.md`.

## Layout

- `catalog/`: operation model and capability graph (resources, schemas, OpenAPI links)
- `agentmeta/`: `agent.yaml` overlay (confirmation, permissions, exposure)
- `replay/`: read OpenTelemetry spans for one run
- `openapi/`: kin-openapi loader
- `semantics/`: derived synonyms and yaml overlay
- `runctx/`: context pack builder (never embeds the raw spec)
- `policy/`: allow, check, confirmation state
- `agent/`: one loop: model, policy, execute, result back to the model
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
