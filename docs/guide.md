# Using veto

Point `veto` at OpenAPI files you already have. The agent gets three tools. Destructive calls wait for confirmation.

## Install

```bash
go install github.com/aiveto/veto/cmd/veto@latest
```

That installs the `veto` command from `github.com/aiveto/veto/cmd/veto`. From a checkout of this repo, `go run ./cmd/veto` is the same binary.

The container image builds that binary. Running the image runs `veto --help`.

```bash
docker build -t veto .
docker run --rm veto
```

Orders and customers in `testdata` are the first successful path. From the checkout, with `veto` on your `PATH`:

```bash
veto doctor --config testdata/veto.yaml
veto check --config testdata/veto.yaml --case testdata/cases
```

`testdata/veto.yaml` loads `orders.yaml` and `customers.yaml`. Doctor reports that `orders.delete` requires approval and exits zero. Check prints the joins and the case results.

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

OpenAPI says what a call requires. `veto.yaml` says how this deployment obtains it. `source: command` is the adapter for a scheme this binary does not speak. `source: token_exchange` trades a subject token for an access token scoped to one audience.

A token response can carry two secrets. `access_token` is sent as `Authorization: Bearer`. The user token defaults to `id_token` and is sent on `user_header`. Set `auth_token` and `user_token` when the JSON fields have other names. Both headers go out when `user_header` is set, or the contract sets `x-user-token-header` on the scheme. A client-credentials token is never placed on that user header. If the user token is required and login did not store it, the call fails before upstream HTTP. The identity provider or the API checks the token. Veto stores the field and sends it.

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

An MCP host that already has the user token passes `token` on `capabilities_invoke`. That value is sent only when the scheme says `source: invoke`. Token exchange can use that same token as `subject: invoke`, or name a login scheme. The upstream call sends the exchanged access token. A missing subject fails before the token URL and before upstream HTTP.

```yaml
auth:
  upstream:
    source: token_exchange
    token_url: https://idp.example/oauth/token
    client_id: veto
    client_secret_env: VETO_SECRET
    audience: https://api.example
    scopes: [orders.read]
    subject: invoke
```

Anything else is a command. Veto writes JSON to its stdin (`operation_id`, `method`, `url`, `scheme`, and `user_token` when this invoke has one) and reads `headers` plus optional `expires_at` from stdout. A non-zero exit, a timeout, or bad JSON fails the invoke before upstream HTTP. Stdin and stdout are not logged.

The command receives the method and the URL. It does not receive the body, so it cannot sign one. `Credential.Sign` sees the finished request, after the body and the URL are set.

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

`operation_id` is the catalog id. `params` is an object. Path, query, and header values are strings, keyed by parameter name. `body` is the request body: a string is sent as written, and a JSON object is encoded as JSON and sent. There is no schema compiler in that step. `approval_id` is empty on the first call. A `confirmation_required` result carries a pending id. That id does not run the call. `veto approve <id>` records the approval and prints an approved id. A later invoke with that approved id runs once.

`response_fields` in `veto.yaml`, or `fields` on this invoke, names the JSON fields returned after a successful call. A list also returns `page` with `offset`, `limit`, and `returned`. `truncated` is true when the response cap cuts the body or the page stops before the end. `response_limit` is the page size when the invoke omits `limit`. With no fields named, the body is unchanged. The cap stays. `--keep-sensitive` is still what records a response body, and secrets are still removed from the trace.

```yaml
response_fields:
  - id
  - name
response_limit: 20
```

`approval_webhook` names a command or an HTTP URL. When a call is pending, veto POSTs JSON, or writes the same JSON to the command's stdin:

```json
{"id":"<pending id>","operation":"orders.delete","caller":"ada"}
```

The message is the pending id, the operation, and the caller. It leaves out parameters, secrets, and upstream tokens. `veto approve` is still the local approval. The model's next call is not an approval.

## Remote MCP

Stdio stays the default. `--http` serves the same three tools over Streamable HTTP.

```bash
veto serve --config veto.yaml --http --addr 127.0.0.1:7433
```

`127.0.0.1:7433` is the address when `--addr` is omitted. Every request sends the caller credential in the `Veto-Caller` header:

```text
Veto-Caller: <credential>
```

That header is who is calling veto. The upstream API token stays in `auth` or in the invoke `token` argument. Traces do not record the caller credential.

Name each caller in `veto.yaml`. The value is the environment variable that holds the credential.

```yaml
callers:
  ada: ADA_CALLER_TOKEN
  grace: GRACE_CALLER_TOKEN
```

One caller cannot use another caller's approval id or token. Invoke still runs policy before any upstream HTTP. `preview` sends no upstream HTTP. A pending approval id is not approval.

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
          go-version-file: go.mod
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

## Policy

`policy: builtin` is the default. `policy: opa` reads `policy_file` or `policy_bundle`.

```yaml
policy: opa
policy_file: policy.rego
environment: prod
caller: ada
approval_webhook: https://hooks.example/pending
```

A command is the other webhook form: `approval_webhook: /usr/local/bin/veto-notify`.

Rego sees `operation`, `params`, `method`, `path`, `side_effect`, `permissions`, `caller`, `environment`, `auth_scheme`, `tags`, and `resource_group`. A fact the call does not have is empty. `environment` is the config value. `caller` is the invoke caller, or `caller` in config when the invoke omits one. `auth_scheme` is the scheme names on the operation. `path` is the contract path. `resource_group` is the catalog group.

A deny stops the call before HTTP. When Rego allows the call, builtin permissions and confirmation still apply.

## Replay

```bash
veto replay --config veto.yaml --message "delete order 123"
veto replay --from trace.json
```

The first command runs the message and prints the trace. With the default model that sentence is `orders.delete`, then `decision=confirmation_required`, then a pending id. `veto approve` records the yes. With `VETO_APPROVAL_SECRET` the approved id is a signed token, and a consumed nonce is kept on this machine. The user message is not on the trace. HTTP method and status show up when a call is sent. Set `trace_file: trace.json` in `veto.yaml` to save the trace, then `--from` prints that file. `--keep-sensitive` records response bodies. Leave it off to keep them out.

## Doctor

`veto doctor --config veto.yaml` loads the contracts, relations, and agent file, then reports:

- a `--pin` that is unknown or discovery-only
- an operation whose required auth is missing, and a security scheme whose env var, client secret, or stored login is missing
- a colliding operation id, and an id filled in when the contract omitted `operationId`
- a parameter that veto cannot place on the request
- an empty summary, or a weak summary (one word, or the same as the operation id)
- a write that requires approval
- with `--ping`, a GET that fails for a server URL

It exits non-zero when a finding would make a call wrong: missing auth, a colliding id, or a parameter that cannot be serialized. A fallback id, a weak summary, and a write that requires approval are reported and do not by themselves fail the command. It does not evaluate policy, and it does not approve a delete.

## Preview

```bash
veto preview --config veto.yaml --operation orders.delete --param id=123
```

Preview runs resolve, validate, and policy, then stops. It does not call upstream HTTP and it does not call a token URL. The JSON result is the operation, the request with secrets removed, validation errors, the policy decision, and whether approval is required.

`capabilities_invoke` with `preview` set to true returns that same result. The CLI and MCP both call the invoke runtime.

## Generate

```bash
veto generate --config veto.yaml --out ./client --module example.com/client
```

The generated client calls through the same gate.

## Orders and customers

`examples/two-apis` is two contracts. Each file keeps its own server URL.

```yaml
contracts:
  - orders.yaml
  - customers.yaml
```

A person logs in once. Invoke exchanges that token for the bearer the contracts name.

```yaml
contracts:
  - orders.yaml
  - customers.yaml
auth:
  user:
    source: login
    client_id: veto
    issuer: https://idp.example
    scopes: [orders.read]
  bearerAuth:
    source: token_exchange
    token_url: https://idp.example/oauth/token
    client_id: veto
    client_secret_env: VETO_SECRET
    audience: https://orders.example
    scopes: [orders.read]
    subject: user
```

```bash
veto auth login --config veto.yaml --scheme user
```

OPA can require confirmation for the write `orders.delete`. Builtin confirmation still applies when Rego allows the call.

```yaml
policy: opa
policy_file: policy.rego
```

```rego
package veto

import rego.v1

default decision := "allow"
default reason := ""

decision := "confirmation" if {
	input.operation == "orders.delete"
}
```

A command can supply `bearerAuth`. Veto writes JSON to its stdin and reads headers from stdout.

```yaml
auth:
  bearerAuth:
    source: command
    command: /usr/local/bin/veto-sig
    timeout: 5s
```

`veto check` on a pull request, for that same `veto.yaml`:

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
          go-version-file: go.mod
      - run: go install github.com/aiveto/veto/cmd/veto@latest
      - run: veto check --config veto.yaml --case cases --against ${{ github.event.pull_request.base.sha }}
```

## Limits

`veto serve` is stdio MCP. There is no shared listener.

Approval nonces are local files. `VETO_APPROVAL_SECRET` signs the yes. `VETO_APPROVAL_NONCE_DIR` is the nonce directory on that machine. Two machines that share the signing secret and not the nonce directory can both accept a yes until expiry.

Token files are local. `token_dir` and `VETO_TOKEN_DIR` name that directory. They are not a remote session store.

```text
agent host
    |  stdio MCP
    v
  veto
    |  your API's HTTP
    v
 your API
```

Veto is not backend auth. The API still authenticates the caller and enforces its own authorization.

Veto is not an agent framework. The host owns the model.

Veto is not an API gateway replacement. It does not sit in front of every client.

## Roadmap

HTTP transport. One approval webhook. Richer policy input. Better discovery.

## What the API still owns

The service still authenticates the caller, stores the data, and enforces its own authorization and quotas. Veto names the env var and holds a destructive call until confirmation is stored.
