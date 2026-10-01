# veto

Veto turns existing OpenAPI services into three tools for an agent: search, describe, and invoke (`capabilities_search`, `capabilities_describe`, `capabilities_invoke`).

Auth, approval, and relations are checked before the API sees the request. Replay and drift checks read the recorded decision.

```text
API contracts
     ↓
Catalog
     ↓
Search
     ↓
Context pack
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

Orders, customers, and payments can be hundreds of operations. The model gets three:

```text
capabilities_search
capabilities_describe
capabilities_invoke
```

It searches, reads one description, and invokes through policy. It does not get one tool for every operation. Pin lists a few operations beside those three. Group adds one tool per resource.

## The delete waits

```text
Delete order 123
     ↓
orders.delete
     ↓
confirmation required
     ↓
veto approve <pending id>
     ↓
the approved id runs once
```

The pending id does not send HTTP. `veto approve` records the yes. The approved id is one use.

## The next API is declared

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

`orders.get` returns a customer id. The relation names `customers.get` as the next call. `capabilities_invoke` runs one operation. Follow runs that next call. Veto does not guess joins from field names. The raw spec stays out of the model.

```bash
git clone https://github.com/aiveto/veto.git
cd veto
go test ./...
go run ./examples/two-apis
```

`examples/two-apis` starts the sample APIs, checks the delete path, and exits. It is not a session you attach to. The session that stays up is [veto-demo](https://github.com/aiveto/veto-demo): Harbor's orders, customers, and billing, and `make demo`.

`veto serve` needs an API that is still listening and a credential source for that contract:

```bash
go run ./cmd/veto validate --contract testdata/openapi.yaml
go run ./cmd/veto eval --contract testdata/openapi.yaml --case testdata/delete.yaml
go run ./cmd/veto serve --contract testdata/openapi.yaml --stdio
```

`serve --stdio` speaks MCP on stdin. Do not point it at `examples/two-apis/veto.yaml` after the example has exited. That file expects `bearerAuth` and a process that is still up.

Setup is in [docs/guide.md](docs/guide.md).

`veto check --against` fails when a confirmation disappears, a joined call is gone, or a new destructive operation appears. `veto replay` shows the decision. The user message stays off the trace.

Veto is not backend authorization, and it is not an agent framework. Pre-1.0. Apache-2.0.
