# Veto

**Turn existing OpenAPI services into tools AI agents can discover and call under your rules.**

The model may request a call. Veto checks policy, requires approval for a destructive call, and resolves credentials before your API runs. Declared relations name the next operation. The decision is recorded.

**The model proposes. Veto decides. Your API executes.**

Connect an MCP client, or embed the Go runtime. MCP, the CLI, eval, and a generated Go client share that runtime. The client calls the catalog through search, describe, and invoke. A hundred endpoints do not become a hundred tools. Your services stay where they already run.

## See veto in action

[veto-demo](https://github.com/aiveto/veto-demo) is the full walk. Harbor sells home goods. Orders, customers, and billing are the APIs. The walk follows a customer from an order, holds a delete until a person approves it, and keeps the secret out of the trace. `make demo` runs the story. `make mcp` leaves Harbor listening and prints the config for Claude, Cursor, or ChatGPT.

The same path runs in this repo, then exits. Go 1.27.1. No model key.

```bash
git clone https://github.com/aiveto/veto.git
cd veto
go run ./examples/two-apis
```

```text
Someone asked who placed order 123.
orders.get returned customerId 7.
customers.get was called for 7 because the note said Order.customerId identifies customers.get.
orders.delete sent no HTTP until approved.
The trace left the secret out.
```

## A delete waits

```text
Agent requests the call        -> pending ID; no upstream HTTP
Person runs veto approve <id>  -> approved ID
Agent submits the approved ID  -> one matching invocation
```

The approval is bound to the caller, the operation, and the parameters. Permission and confirmation run before credentials are fetched and before HTTP. An [OPA](docs/guide.md#policy) allow does not skip those checks. A webhook or a command can notify your approval system. The server and `veto approve` share approval storage and signing configuration.

Once the call is allowed, timeouts, retries, and a per-caller limit still apply.

## The next call is declared

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

Search returns the related operation. Describe returns the note, such as `Order.customerId identifies customers.get`. The Go Follow API walks that link. MCP invoke runs one operation. [Relations](docs/guide.md#relations).

## What the agent receives

The tool names are `capabilities_search`, `capabilities_describe`, and `capabilities_invoke`. A pin or a resource group can sit beside those three. Search matches the summary, tags, path, and synonyms such as retire for delete. An overlay can add a word of your own.

[Response shaping](docs/guide.md#mcp) returns named fields and a bounded list, and marks pagination and truncation. A Go [context pack](docs/guide.md#pack) holds rules, operation summaries, the conversation, relations, and a pending confirmation, inside a byte budget. The raw OpenAPI document stays out of the pack.

## Connect your services

One [veto.yaml](docs/guide.md#one-vetoyaml) names the OpenAPI files and the credential sources. `veto serve --stdio` speaks MCP on stdin. [Authenticated Streamable HTTP](docs/guide.md#remote-mcp) serves the same runtime to a remote client.

Credentials come from the environment, OAuth, a caller-supplied token, token exchange, a command that prints headers, or a Go provider that signs the request. [Authentication](docs/guide.md#auth).

## Check it

| Task | How |
| --- | --- |
| Load the contracts | [`validate`](docs/guide.md#one-vetoyaml) |
| Missing auth, operation IDs, unsupported parameters | [`doctor`](docs/guide.md#doctor) |
| Request and policy decision, before credentials and before HTTP | [`preview`](docs/guide.md#preview) |
| Run cases, and fail when a join, a confirmation, a permission, or a destructive operation drifts | [`eval`](docs/guide.md#check-in-ci) and [`check --against`](docs/guide.md#check-in-ci) |
| Read a saved trace, or run a message | [`replay --from`](docs/guide.md#replay) prints the file; `replay` with a message executes it |
| Share contracts, relations, and cases apart from deployment credentials | [capability bundle](docs/guide.md#capability-bundle) |
| Call the same runtime from your own Go module | [generate](docs/guide.md#generate) a client, a CLI, and a dispatch package |

Traces are OpenTelemetry, with OTLP export. The Go model and memory interfaces, and sequential flows, run in-process. They are not a durable workflow service.

```bash
go run ./cmd/veto validate --contract testdata/openapi.yaml
go run ./cmd/veto eval --contract testdata/openapi.yaml --case testdata/delete.yaml
go run ./cmd/veto serve --contract testdata/openapi.yaml --stdio
```

`validate` and `eval` read the contract in this repo. `serve --stdio` is the MCP process. A call needs an API that is still listening, which is what veto-demo keeps up.

## Scope

**Pre-1.0.** Public APIs may change. OpenAPI support is a subset: not full schema validation, and not every auth scheme. Approvals and tokens are stored on the machine that issued them. A shared signing key does not make an approval single-use across machines.

Your host owns the model. Your APIs own business logic and authorization.

[Setup guide](docs/guide.md) | [Current limits](docs/guide.md#limits) | [Apache-2.0](LICENSE)
