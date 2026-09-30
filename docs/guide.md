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

Paths are relative to `veto.yaml`. `auth` maps a security scheme to an env var, a login, client credentials, or a command. Secrets stay out of this file. Unset keys keep the defaults (`model: scripted`, `policy: builtin`, in-process execution).

`veto validate --config veto.yaml` loads the contracts and prints the operation count and joins.

## Auth

A string names the environment variable that already holds the token:

```yaml
auth:
  bearerAuth: ORDER_TOKEN
```

`veto auth set --config veto.yaml --scheme bearerAuth` reads a pasted token from stdin and stores it mode `0600`. `token_dir` sets the directory. `VETO_TOKEN_DIR` overrides it, so a cluster can mount that path.

A person signs in once. `issuer` loads `authorization_endpoint` and `token_endpoint` from `.well-known/openid-configuration` when you do not want to copy both URLs.

```yaml
auth:
  user:
    source: login
    client_id: veto
    authorization_url: https://idp.example/authorize
    token_url: https://idp.example/oauth/token
    scopes: [orders.read]
```

```bash
veto auth login --config veto.yaml --scheme user
veto auth login --config veto.yaml --scheme user --device
```

The browser opens from login. Register `http://127.0.0.1:53682/callback` at the identity provider, or set `redirect_url`. Invoke reads the refresh token, refreshes it near expiry, and does not open a browser. No stored token fails the call before upstream HTTP.

A token response can carry two secrets. `access_token` is sent as `Authorization: Bearer`. The user token defaults to `id_token` and is sent on `user_header`. Set `auth_token` and `user_token` when the JSON fields have other names. Both headers go out when `user_header` is set, or the contract sets `x-user-token-header` on the scheme. A client-credentials token is never placed on that user header. If the user token is required and login did not store it, the call fails before upstream HTTP. A JWT user token is checked for issuer, audience, and expiry when discovery published `jwks_uri`. An opaque user token is sent as-is.

```yaml
auth:
  user:
    source: login
    client_id: veto
    issuer: https://idp.example
    user_header: X-User-Token
    user_token: person_token
```

Workforce and CI use client credentials. The secret stays in the environment. There is no browser.

```yaml
auth:
  workforce:
    source: client_credentials
    token_url: https://idp.example/oauth/token
    client_id: veto-job
    client_secret_env: WORKFORCE_SECRET
    scopes: [orders.read]
    audience: https://api.example
```

An MCP host that already has the user token passes `token` on `capabilities_invoke`. That value is sent only when the scheme says `source: invoke`.

Anything else is a command. Veto writes JSON to its stdin (`operation_id`, `method`, `url`, `scheme`, and `user_token` when this invoke has one) and reads `headers` plus optional `expires_at` from stdout. A non-zero exit, a timeout, or bad JSON fails the invoke before upstream HTTP. Stdin and stdout are not logged.

```yaml
auth:
  sig:
    source: command
    command: /usr/local/bin/veto-sig
    timeout: 5s
```

`apiKey` schemes use the header or query the contract names. An operation with `security: []` sends no credential. Two schemes in one requirement set two headers. Policy runs before any token URL or command.

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
- a security scheme that is not configured, or whose env var, client secret, or stored login is missing
- with `--ping`, a GET that fails for a server URL

It does not evaluate policy, and it does not approve a delete.

## Generate

```bash
veto generate --config veto.yaml --out ./client --module example.com/client
```

The generated client calls through the same gate.

## What the API still owns

The service still authenticates the caller, stores the data, and enforces its own authorization and quotas. Veto names the env var and holds a destructive call until confirmation is stored. It does not replace the API, and it is not an agent framework.
