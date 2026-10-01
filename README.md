# veto

Veto turns existing OpenAPI services into a bounded, policy-checked capability surface for AI agents.

Agents search, describe, and invoke through three stable tools. Veto handles auth, approval, relations, replay, and CI drift checks before your API ever sees the request.

```text
API contracts
     ↓
Capability catalog
     ↓
Discover only what is relevant
     ↓
Bounded context
     ↓
Policy
     ↓
Approval
     ↓
The API call
     ↓
Trace and test
```

The model proposes. Veto decides. Your API executes.

## Three tools

Orders, customers, and payments can be hundreds of operations. The agent gets three:

```text
capabilities_search
capabilities_describe
capabilities_invoke
```

It searches, reads one description, and invokes through policy. Pin or group a few when you need them.

## The delete waits

```text
Delete order 123
     ↓
orders.delete
     ↓
confirmation required
     ↓
NO HTTP REQUEST
```

Approval checks the same call again. Then it is sent.

## The next API is declared

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

`orders.get` returns a customer id. `customers.get` runs because that line says so. Veto does not guess joins from field names. The raw spec stays out of the model.

```bash
go install github.com/aiveto/veto/cmd/veto@latest
go run ./examples/two-apis
go run ./cmd/veto serve --config examples/two-apis/veto.yaml --stdio
```

A declared `Content-Type` such as `application/json;v=3` is sent as written. Setup is in [docs/guide.md](docs/guide.md).

`veto check --against` fails when a confirmation disappears, a joined call is gone, or a new destructive operation appears. `veto replay` shows the decision. The user message stays off the trace.

Veto is not backend authorization, and it is not an agent framework. Pre-1.0. Apache-2.0.
