# Using veto

Point `veto` at OpenAPI files you already have. The agent gets three tools. Destructive calls wait for confirmation.

## Install

```bash
brew install aiveto/veto/veto
```

```bash
go install github.com/aiveto/veto/cmd/veto@latest
```

`brew` does not need Go. `go install` needs Go 1.27.1. From a checkout of this repo, `go install ./cmd/veto` or `go run ./cmd/veto`.

The container image builds `veto`. Running the image runs `veto --help`. A tagged release pushes `ghcr.io/aiveto/veto`.

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

## Commands

| Task | How |
| --- | --- |
| The catalog loads, and its operation count and joins are printed | [validate](#one-vetoyaml) |
| Missing auth, a colliding operation id, or a parameter that cannot be sent | [doctor](#doctor) |
| The request and the policy decision, before a token is fetched and before HTTP | [preview](#preview) |
| Run a case | [eval](#check-in-ci) |
| Fail when a joined operation disappears, confirmation or a permission is dropped, a new destructive operation appears, or a case expectation changes. `confirmation: false` is the record of a deployment-wide drop | [check --against](#check-in-ci) |
| Print a saved trace | [replay --from](#replay) |
| Run a message | [replay](#replay) |
| Share contracts, relations, and cases apart from deployment credentials | [capability bundle](#capability-bundle) |
| A read-only task the agent can find. Check fails when its binding or answer field is gone | [tasks](#tasks) |
| Call the same runtime from your own Go module | [generate](#generate) a client, a CLI, and an MCP dispatch package |

Traces are OpenTelemetry. OTLP export is optional. The Go model and memory interfaces, and sequential flows, run in-process. They are not a durable workflow service.

## One veto.yaml

```bash
veto init orders.yaml customers.yaml
```

This writes `veto.yaml` in the current directory and, if it is missing, a `relations.yaml` stub (`relations: []`). When a contract loads and a read lists response fields, it also writes `flow.yaml` for that read and sets `flow_file`. A declared relation between two reads becomes the two steps. An existing `flow.yaml` is left in place. If `veto.yaml` is already there, `veto init` stops and leaves the files alone.

When that read declares a security scheme, `init` writes `auth` with an environment variable for the scheme and prints the variable when it is unset. It writes `agent.yaml` naming those operations and sets `agent_file`. It previews the first read. It calls that read when the preview is clean and the credential is set. The response is not printed.

```yaml
contracts:
  - orders.yaml
  - customers.yaml
relations_file: relations.yaml
```

That is the file `init` writes. Paths are relative to `veto.yaml`. An `http` or `https` URL is fetched as written. Add `auth` when a scheme needs a credential. A string names the environment variable that holds the token. A login, client credentials, or a command is the other form. Secrets stay out of this file. Unset keys keep the defaults (`model: scripted`, `policy: builtin`, `json: v2`). `json: v1` is `DefaultOptionsV1` for JSON sent to a client API or tool. Tokens and approvals stay on v2.

```yaml
auth:
  bearerAuth: ORDER_TOKEN
```

`confirmation: false` turns the confirmation gate off for every operation in this deployment. Unset, and `confirmation: true`, leave it on. The clear runs after `agent.yaml`, so one operation set to true does not turn the gate back on. When the key is unset, `agent.yaml` can still set `confirmation: false` on one operation. Invoke cannot set the key. A bundle manifest cannot carry it. Doctor and check print `confirmation is off`. A semantics file that names an operation the catalog does not have fails to load.

`chat_approval: true` lets a remote `--http` client answer the confirmation form. Unset, stdio still asks when the host supports elicitation, and `--http` returns the pending id for `veto approve`. A bundle manifest cannot carry it.

`read_only: true` and `expose` serve part of a contract. Unset serves every operation.

```yaml
read_only: true
expose:
  tags: [orders]
  paths: [/orders, /customers]
```

`read_only` keeps GET and HEAD. `expose.tags` keeps an operation with one of those tags. `expose.paths` keeps an operation whose path is that prefix or sits under it, so `/orders` keeps `/orders/{id}` and not `/orders-archive`. An operation must pass every key that is set. A removed operation is gone from search, describe, invoke, the pack, and the graph, and its relations drop with it. The cut runs after `agent.yaml`. A bundle manifest cannot carry these keys. `veto check --against` applies each side's own keys, so a narrower cut that drops a joined operation fails, and a wider one that adds a destructive operation fails.

`approval_ttl: 30m` sets the lifetime of a signed approval. Unset keeps 15 minutes. It applies when `VETO_APPROVAL_SECRET` is set. `approval_store: VALKEY_URL` reads that env for a Valkey or Redis URL so more than one process shares consume-once. `VETO_APPROVAL_STORE` is the URL itself. A bundle manifest cannot carry these keys.

`veto validate --config veto.yaml` loads the contracts and prints the operation count and joins.

## Capability bundle

A directory, or a zip of that directory, holds the contracts, the relations file, and the check cases for one integration. `bundle.yaml` lists those files the same way `veto.yaml` does. When `bundle.yaml` is absent, `veto.yaml` in that directory is the manifest. Credentials, token URLs, client secrets, and environment base URLs stay in the deployment config. A bundle that contains a client secret, a token URL, or a base URL does not load.

```bash
veto check --bundle ./orders-customers
veto doctor --bundle ./orders-customers.zip --config veto.yaml
veto serve --config veto.yaml
```

Deployment `veto.yaml`:

```yaml
bundle: ./orders-customers
auth:
  bearerAuth: ORDER_TOKEN
```

`bundle.yaml`:

```yaml
contracts:
  - orders.yaml
  - customers.yaml
relations_file: relations.yaml
cases:
  - cases
```

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
      "args": ["serve", "--config", "veto.yaml", "--stdio"],
      "env": {
        "VETO_APPROVAL_NONCE_DIR": "/path/to/approvals"
      }
    }
  }
}
```

The env block is what `veto approve` in another shell must share, or set `approval_store` for Valkey or Redis. [veto-demo](https://github.com/aiveto/veto-demo) `make mcp` prints a complete block.

`veto serve` listens on stdio. The registered tools are `capabilities_search`, `capabilities_describe`, and `capabilities_invoke`. `--pin orders.get` also registers that operation id; the pin schema omits `operation_id`. `--grouped` registers one tool per resource and lists the callable ids in that group's description. A group or pin name cannot replace the three capabilities. Search returns `id`, the pack call line, related ids, `related_calls`, and `confirmation` when the gate is on. Describe still returns the operation.

`veto serve --json` serves JSON lines for skills and scripts. An idle `--json` process polls stdin so SIGTERM stops it. `veto --help-json` is the contract. Catalog flags stay on `serve`.

```bash
veto --help-json
veto serve --config veto.yaml --json
```

```json
{"search":{"query":"retire order 123"}}
{"describe":{"operation_id":"orders.delete"}}
{"invoke":{"operation_id":"orders.delete","params":{"id":"123"}},"caller":"ada"}
```

`veto search`, `describe`, and `invoke` are a shorthand. Words are a search query. A JSON object is the tool arguments.

```bash
veto search --config veto.yaml retire order 123
veto invoke --config veto.yaml '{"operation_id":"orders.delete","params":{"id":"123"}}'
```

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

`operation_id` is the catalog id. `params` is an object. Path, query, and header values are strings, keyed by parameter name. `body` is the request body: a string is sent as written, and a JSON object is encoded as JSON and sent. A JSON body is checked against the request body schema before policy and HTTP. A mismatch returns `invalid_body` with the field path and the reason. A query, path, or header value is checked against its schema. A mismatch returns `invalid_param` with the reason. The value is not echoed. `why` names the stage: `parameter`, `policy`, `catalog`, `upstream`, `missing auth`, or `held until you approve`. A bad body stays on `invalid_body` and the error. `sent` is true when the request reached the transport. `http` is true when a response was received. A lost response can leave `sent` true and `http` false; read both before retrying a mutation. `idempotency_key` is a stable retry key for that case. `caller` is who asked. `approval_id` is empty on the first call. A `confirmation_required` result carries a pending id. That id does not run the call. Submitting it as `approval_id` returns `pending_approval` and still does not call HTTP. Over stdio, a host that supports elicitation asks the person to accept or decline. The form is a confirm schema. Accept records the approval and runs the call. Decline leaves the pending id. `veto invoke` asks the same sentence when stdin and stdout are a terminal; `y` records the approval and runs the call once. A skill or `veto serve --json` has no terminal pair, so it returns the pending id. Over `--http` that form needs `chat_approval: true` in `veto.yaml`, because the remote client is the one answering it. Otherwise the result carries the pending id, and `veto approve <id>` records the approval and prints an approved id. A later invoke with that approved id runs once.

Approvals default to `policy.Memory` in this process. Serve and `veto approve --config veto.yaml` share `policy.Files` when `VETO_APPROVAL_NONCE_DIR` is set, or the default approval dir. More than one process sets `approval_store` in that file (or `VETO_APPROVAL_STORE`) to a Valkey or Redis URL. `Claim` is SET NX on the pending id. `State.SetStore` takes another `policy.Store`.

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

`127.0.0.1:7433` is the address when `--addr` is omitted. `GET /healthz` and `GET /readyz` answer 200 without a caller credential. They report that this process is up. They do not check the upstream API. Every other request sends the caller credential in the `Veto-Caller` header:

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

A call whose body is `{"name":"ada"}` goes out with `Content-Type: application/json;v=3`.

## Relations

`relations.yaml`:

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

`orders.get` returns `customerId`. The relation names `customers.get` as the linked operation. A field name in the spec does not create that call. A successful invoke includes `next_calls` when the response has the field: the operation id and its path or query params. A missing or projected field is left out, and a header is not copied. The invoke stays successful. A relation that would only join an operation to itself is refused. The same related id is listed once.

An OpenAPI link with no `parameters` map does not invent a call. A link parameter may be a JSON pointer into the response: objects, and one array index.

```yaml
parameters:
  id: $response.body#/customer/id
```

`$response.body#/items/0/id` is the one index. A second index, such as `$response.body#/rows/0/cols/1`, is an error and is not a call.

## Tasks

A flow with a question is a task. Name the read-only steps and the response fields that answer it. A later step takes its parameter from one declared relation. Set `output` and `to` on that step when more than one relation reaches the next operation.

```yaml
name: investigate-charge
question: Investigate a customer's disputed charge
answer: [amount, currency]
steps:
  - orders.get
  - invoices.get
```

`Order.invoiceId` joined to `invoices.get` becomes the binding `orders.get invoiceId -> invoices.get id`. `veto search "disputed charge"` returns the task. The context pack includes that sentence when the message matches the question or an answer field. `veto propose` prints a task for each declared relation between two reads. The question is the relation sentence. The answer lists that operation's response fields for you to keep or delete. The steps do not copy the parameter mapping. `veto run investigate-charge --param id=10482` calls those steps and prints what stayed true: the earlier response supplied the binding field, the next operation accepted that parameter, the last response included the answer fields, and the task stayed read-only. Those lines do not include the response or the identifier. `--save` writes that task. A binding the relation already supplies stays out of the file. With that file as `flow_file`, `veto check` runs the task without an eval case and names it when a field is gone. A step that stops names the stage: parameter, policy, catalog, upstream, missing auth, or held until you approve. A flow step that names an operation, response field, or parameter the catalog does not have fails to load. `veto check` prints the sentence. A `flows` list in that file holds the task next to any other flow. Check fails when the response no longer includes the binding field or an answer field, when the next operation has no parameter for it, or when a step is not a read.

## Pack

```bash
veto pack --config veto.yaml --message "who placed order 123"
```

`--json` prints the same pack as JSON. The command builds a pack for that message: the rules, a one-line index, and the message. It does not attach a described operation or a pending confirmation. Those fields exist on the pack type and are filled during an agent turn. A pending confirmation keeps its id after params are dropped to fit the budget. The raw spec stays out.

## Check in CI

`veto check` loads the catalog, prints joins and any task, and runs the case files. A task in the flow file is the check when no case is configured. Check fails when that task can no longer be taught. `--case` names the case file or directory. When it is omitted, check uses the `cases` list from the bundle. A case is required only when no task is loaded. `--against` is a git ref or a snapshot JSON file. The check fails when a joined operation disappears, confirmation is dropped without an `agent.yaml` change or `confirmation: false`, a new destructive operation appears, a discovery-only operation becomes callable, a required permission is removed, an eval expectation changes, or a task can no longer obtain its binding or an answer field. Drift compares `operation`, `confirmation_required`, `no_http`, `pack_contains`, `pack_excludes`, and `related`. `confirmation: false` prints `confirmation is off` and does not also report each operation as lost confirmation.

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
      - run: go install ./cmd/veto
      - run: veto check --config veto.yaml --case cases --against ${{ github.event.pull_request.base.sha }}
```

That workflow checks out this module. `cases/delete.yaml` is the confirmation case when the contract has `orders.delete`. The default model selects that id from the sentence below, and the call must not reach HTTP:

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

```rego
package veto

import rego.v1

default decision := "allow"
default reason := ""

decision := "confirmation" if {
	input.operation == "orders.delete"
}
```

## Replay

```bash
veto replay --config veto.yaml --message "delete order 123"
veto replay --from trace.json
```

The first command runs the message and prints the trace. With the default model that sentence is `orders.delete`, then `decision=confirmation_required`, then a pending id. `veto approve` records the yes. With `VETO_APPROVAL_SECRET` the approved id is a signed token. Consume-once is still the store. The user message is not on the trace. HTTP method and status show up when a call is sent. Set `trace_file: trace.json` in `veto.yaml` to save the trace, then `--from` prints that file. `--keep-sensitive` records response bodies. Leave it off to keep them out. `trace_export: stdout` and `trace_export: otlp` export the same allowlisted attributes. A response body is not in that set.

## Doctor

`veto doctor --config veto.yaml` loads the contracts, relations, and agent file, then reports:

- a `--pin` that is unknown or discovery-only
- an operation whose required auth is missing, and a security scheme whose env var, client secret, or stored login is missing
- a colliding operation id, and an id filled in when the contract omitted `operationId`
- a parameter that veto cannot place on the request
- an empty summary, or a weak summary (one word, or the same as the operation id)
- a write that requires approval
- `confirmation is off`, when `veto.yaml` sets `confirmation: false`
- with `--ping`, a GET that fails for a server URL. The failure says upstream.

It exits non-zero when a finding would make a call wrong: missing auth, a colliding id, or a parameter that cannot be serialized. A fallback id, a weak summary, a write that requires approval, and `confirmation is off` are reported and do not by themselves fail the command. It does not evaluate policy, and it does not approve a delete.

## Preview

```bash
veto preview --config veto.yaml --operation orders.delete --param id=123
```

Preview runs resolve, validate, and policy, then stops. It does not call upstream HTTP and it does not call a token URL. The JSON result is the operation, the request with secrets removed, validation errors, the policy decision, and whether approval is required. Integer digits in the request body stay as written.

`capabilities_invoke` with `preview` set to true returns that same result. The CLI and MCP both call the invoke runtime.

## Generate

```bash
veto generate --config veto.yaml --out ./client --module example.com/client
```

The generated client calls through the same gate. `New` builds a catalog-faithful client with builtin policy. Links and page maps from the catalog are on that client, so `next_calls` and a page walk match a direct invoke. `SetPolicy` replaces that hook. `Confirm` records a yes. The runtime stays unexported.

A caller that already configured policy, approval state, or pagination passes that runtime in:

```go
client := sdk.NewRuntime(rt)
```

`rt` is the runtime `veto serve` and the agent loop use.

A page walk authorizes every derived request the same way as the first. `page: follow` in `veto.yaml` walks up to five pages. Unset stays one page.

## Limits

`veto serve` defaults to stdio. `--http` is one process at `127.0.0.1:7433`.

`VETO_APPROVAL_SECRET` signs the yes. Consume-once is the store: Files on one machine, Valkey or Redis when `approval_store` is set. Sharing the signing secret is not enough. Consume looks up the approved record in the store, then claims it. Two processes that do not share that store cannot accept the same approval. Replicas need the shared store for lookup and for consume-once.

Token files are local. `token_dir` and `VETO_TOKEN_DIR` name that directory. They are not a remote session store.

`invoke_limit` is calls per second per caller in this process. Unset or 0 is 16. The next call stops before policy, a token URL, and upstream HTTP. A bundle cannot set it.

Veto is not backend auth. The API still authenticates the caller, stores the data, and enforces its own authorization and quotas.

Veto is not an agent framework. The host owns the model.

Veto is not an API gateway replacement. It does not sit in front of every client.
