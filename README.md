# veto

The control plane between AI agents and your APIs.

Your APIs already know how to create, read, update, delete, refund, publish, deploy, and approve things.

The problem is giving an AI agent access to all of that.

Expose every OpenAPI operation as an MCP tool and you get a huge tool surface. Give the model the whole API description and you waste context. Let the model decide what reaches your APIs and a bad decision becomes a real side effect.

Veto puts a controlled boundary between agent intent and API execution.

```text
API contracts
     ↓
Capability catalog
     ↓
Discover only what is relevant
     ↓
Build bounded context
     ↓
Apply policy
     ↓
Require approval when needed
     ↓
Execute the API call
     ↓
Trace and test what happened
```

The model proposes. Veto decides. Your API executes.

## Why Veto

```text
Orders API       80 operations
Customers API   120 operations
Payments API     95 operations
```

Turn each operation into an MCP tool and the agent receives hundreds of tools.

Veto does not. By default the agent gets three capabilities:

```text
capabilities_search
capabilities_describe
capabilities_invoke
```

The agent searches, reads one description, and invokes through the policy boundary. Pin or group a few capabilities when you need them. That is opt-in.

## The call waits

```text
User: Delete order 123
```

The model can name `orders.delete`. Veto does not send `DELETE /orders/123`.

```text
orders.delete
     ↓
destructive operation
     ↓
confirmation required
     ↓
NO HTTP REQUEST
```

After approval, the same operation and parameters are checked again. Then the request is sent. An outer allow still reaches builtin confirmation on a destructive call.

## Your contracts, one catalog

```yaml
contracts:
  - orders.yaml
  - customers.yaml
  - payments.yaml
```

You do not rewrite the APIs. Veto merges the contracts into one catalog. Each API keeps its own server. Duplicate operation IDs are rejected. A spec link is used when it has a parameter mapping. A link with no mapping is not a call.

## Connect two APIs on purpose

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

```text
orders.get
    ↓
Order.customerId
    ↓
customers.get
```

That line is the next call. Veto does not guess the join from the field name.

## The model does not get the file

The pack has the capabilities for this request, their neighbors, the declared relations, and the current conversation. The raw OpenAPI document stays out.

```bash
veto pack \
  --config veto.yaml \
  --message "Who placed order 123?"
```

The default memory forgets the conversation when the process stops. `memory: file` keeps turns.

## Test the agent surface

An API change can be valid for a normal client and still change what an agent is allowed to do. Confirmation removed, a joined call deleted, or a new destructive operation is a failed check.

```bash
veto check \
  --config veto.yaml \
  --case testdata/cases \
  --against HEAD
```

## See what happened

```bash
veto replay --from trace.json
```

Replay shows the operation, the policy decision, and the HTTP result. The user message and parameter values stay off the trace. `--keep-sensitive` records response bodies. Veto uses OpenTelemetry.

```bash
veto doctor
```

Doctor checks contracts, relations, auth env names, and pins. It does not print secrets.

## Try it

```bash
go run ./examples/two-apis

go run ./cmd/veto validate \
  --config examples/two-apis/veto.yaml

go run ./cmd/veto pack \
  --config examples/two-apis/veto.yaml \
  --message "who placed order 123"

go run ./cmd/veto serve \
  --config examples/two-apis/veto.yaml \
  --stdio

go test ./...
```

`veto generate` writes a Go client and a CLI. The parameters are strings.

## What Veto owns

Veto owns capability discovery, the MCP surface, bounded context, declared relations, invocation policy, confirmation, parameter checks, replay, and agent-surface checks.

Your API platform still owns backend authentication, backend authorization, row-level access, network controls, rate limits, service-level validation, and secrets.

## What Veto is not

Veto is not a general-purpose agent framework. It does not add a graph engine, vector search, a database, subagents, an API gateway, or a second tracing format.

Give an agent access to API capabilities without giving the model the API surface or the final execution decision.

## Status

Veto is pre-1.0. The catalog, the three tools, relations, confirmation, auth, retries, pagination, replay, evals, the Go client, and agent-surface checks are implemented. The public API and the config format can change before 1.0.

## Contributing

Small packages, explicit control flow, narrow interfaces, and tests that lock behavior.

```bash
go test ./...
```

Then read `docs/adr/`, `docs/before-open-source.md`, `CONTRIBUTING.md`, and `SECURITY.md`.

## License

Apache-2.0
