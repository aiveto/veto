<img src="docs/veto-social.png" width="1280" alt="veto makes it possible for an AI agent to call your API with context and semantics. A hundred endpoints stay 3 tools.">

**Turn existing OpenAPI services into tools AI agents can discover and call under your rules.**

The model may request a call. Veto checks policy, holds a destructive call until confirmation is stored, then fetches credentials and sends HTTP. A declared relation becomes the next call. A trace records the decision.

**The model proposes. Veto decides. Your API executes.**

```bash
go install github.com/aiveto/veto/cmd/veto@latest
```

Needs Go 1.27.1. Binaries are on [GitHub Releases](https://github.com/aiveto/veto/releases).

## See it

[veto-demo](https://github.com/aiveto/veto-demo) is Harbor: orders, customers, billing. `make demo` walks a customer from an order, holds a delete, and keeps the secret off the trace. `make mcp` leaves Harbor up and prints the config for Claude, Cursor, or ChatGPT.

This repo runs the same idea and exits. No model key.

```bash
git clone https://github.com/aiveto/veto.git
cd veto
go run ./examples/two-apis
```

```text
Someone asked who placed order 123.
orders.get returned customerId 7.
customers.get ran because Order.customerId identifies customers.get.
orders.delete sent no HTTP until approved.
The trace left the secret out.
```

## A delete waits

The agent gets a pending id. No upstream HTTP. `veto approve <id>` prints an approved id. The next invoke with that id runs once. Over stdio, a host that can show a form may ask in the chat instead.

The yes is bound to the caller, the operation, and the parameters. Permission and confirmation run before any token URL and before HTTP. An [OPA](docs/guide.md#policy) allow does not skip them.

## The next call is declared

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

Search and describe return that sentence. MCP invoke still runs one operation.

The tools are `capabilities_search`, `capabilities_describe`, and `capabilities_invoke`. Not one tool per endpoint.

## Use it

```bash
veto init orders.yaml customers.yaml
veto serve --stdio
```

`veto.yaml` names the contracts. Credentials stay in the environment. The agent does not log in. [Authentication](docs/guide.md#auth). Remote MCP is [Streamable HTTP](docs/guide.md#remote-mcp).

From this checkout:

```bash
veto validate --config testdata/veto.yaml
veto eval --config testdata/veto.yaml --case testdata/delete.yaml
veto serve --config testdata/veto.yaml --stdio
```

`validate` loads the catalog. `eval` is the held-delete case. `serve` needs an API that is still listening — that is what the demo keeps up.

`doctor`, `preview`, `check --against`, `replay`, bundles, and `generate` are in the [setup guide](docs/guide.md).

## Scope

Pre-1.0. A JSON body is checked against its schema. Approvals are in this process, on disk for `veto approve`, or in Valkey or Redis when you set `approval_store`. Your host owns the model. Your API still authenticates the caller.

[Setup guide](docs/guide.md) | [Limits](docs/guide.md#limits)
