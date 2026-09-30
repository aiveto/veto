# Using veto

Point `veto` at OpenAPI files you already have. The agent gets three tools. Destructive calls wait for confirmation.

```bash
go install github.com/aiveto/veto/cmd/veto@latest
```

That installs the `veto` command from `github.com/aiveto/veto/cmd/veto`. The module's `go` line is `1.26.0`. From a checkout of this repo, `go run ./cmd/veto` is the same binary.

## One veto.yaml

```bash
veto init orders.yaml customers.yaml
```

This writes `veto.yaml` in the current directory and, if it is missing, a `relations.yaml` stub (`relations: []`). If `veto.yaml` is already there, `veto init` stops and leaves both files alone.

```yaml
contracts:
  - orders.yaml
  - customers.yaml
relations_file: relations.yaml
auth:
  bearerAuth: ORDER_TOKEN
```

Paths are relative to `veto.yaml`. `auth` names the environment variable for a contract security scheme. The token stays in the environment. Unset keys keep the defaults (`model: scripted`, `policy: builtin`, in-process execution).

`veto validate --config veto.yaml` loads the contracts and prints the operation count and joins.

## MCP

Cursor and Claude Desktop both take this server entry. Use a config path the `veto` process can read.

```json
{
  "mcpServers": {
    "veto": {
      "command": "veto",
      "args": ["serve", "--config", "veto.yaml", "--stdio"]
    }
  }
}
```

`veto serve` listens on stdio. The registered tools are `capabilities_search`, `capabilities_describe`, and `capabilities_invoke`. `--pin orders.get --direct-pins` also registers that operation id. `--grouped` registers one tool per resource.

`capabilities_invoke` arguments:

```json
{
  "operation_id": "customers.create",
  "params": {
    "id": "123",
    "body": {"name": "ada"}
  },
  "approval_id": ""
}
```

`operation_id` is the catalog id. `params` is an object. Path, query, and header values are strings, keyed by parameter name. `body` is the request body: a string is sent as written, and a JSON object is encoded as JSON and sent. There is no schema compiler in that step. `approval_id` is empty on the first call. A `confirmation_required` result carries an id; send that id with the same operation and params to run the call.

## The version header

The `Content-Type` on the wire is the contract media type. `examples/two-apis/version.yaml`:

```yaml
openapi: 3.0.3
info: {title: Customers, version: "3"}
servers:
  - url: http://127.0.0.1:9
paths:
  /customers:
    post:
      operationId: customers.create
      requestBody:
        required: true
        content:
          application/json;v=3:
            schema: {type: object}
      responses:
        "201": {description: created}
```

A call whose body is `{"name":"ada"}` goes out with `Content-Type: application/json;v=3`. The orders and customers example (`go run ./examples/two-apis`) still follows `orders.get` to `customers.get` and holds `orders.delete` until approval.

## Relations

`relations.yaml`:

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

`orders.get` returns `customerId`. `customers.get` runs because `to:` says so. A field name in the spec does not create that call.

An OpenAPI link with no `parameters` map does not invent a call. A link parameter may be a JSON pointer into the response: objects, and one array index.

```yaml
parameters:
  id: $response.body#/customer/id
```

`$response.body#/items/0/id` is the one index. A second index, such as `$response.body#/rows/0/cols/1`, is an error and is not a call.

## Pack

```bash
veto pack --config veto.yaml --message "who placed order 123"
```

`--json` prints the same pack as JSON. The pack holds the rules, a one-line index, the message, the operation just described, and a pending confirmation. It does not contain the raw spec.

## Check in CI

`veto check` loads the catalog, prints joins, and runs the case files. `--case` is required. `--against` is a git ref or a snapshot JSON file. The check fails when a joined operation disappears, confirmation is dropped without an `agent.yaml` change, a new destructive operation appears, a discovery-only operation becomes callable, a required permission is removed, or an eval expectation changes.

`--against` reads files from that git ref, so the checkout needs the history.

```yaml
name: veto
on:
  pull_request:
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26.0"
      - run: go install github.com/aiveto/veto/cmd/veto@latest
      - run: veto check --config veto.yaml --case cases --against ${{ github.event.pull_request.base.sha }}
```

`cases/delete.yaml` is the confirmation case when the contract has `orders.delete`. The default model selects that id from the sentence below, and the call must not reach HTTP:

```yaml
name: delete-requires-confirmation
input: "Delete order 123"
expect:
  confirmation_required: true
  operation: orders.delete
  no_http: true
```

A case with no `expect` still satisfies `--case` so the catalog diff runs:

```yaml
name: catalog
input: surface
```

`veto eval --config veto.yaml --case cases` runs the cases without the diff.

## Replay

```bash
veto replay --config veto.yaml --message "delete order 123"
veto replay --from trace.json
```

The first command runs the message and prints the trace. With the default model that sentence is `orders.delete`, then `decision=confirmation_required`, then an approval id. The user message is not on the trace. HTTP method and status show up when a call is sent. Set `trace_file: trace.json` in `veto.yaml` to save the trace, then `--from` prints that file. `--keep-sensitive` records response bodies. Leave it off to keep them out.

## Doctor

`veto doctor --config veto.yaml` loads the contracts, relations, and agent file, then reports blockers for:

- a `--pin` that is unknown or discovery-only
- a security scheme with no `auth` env name, or that variable unset
- with `--ping`, a GET that fails for a server URL

It does not evaluate policy, and it does not approve a delete.

## Generate

```bash
veto generate --config veto.yaml --out ./client --module example.com/client
```

The generated client calls through the same gate.

## What the API still owns

The service still authenticates the caller, stores the data, and enforces its own authorization and quotas. Veto names the env var and holds a destructive call until confirmation is stored. It does not replace the API, and it is not an agent framework.
