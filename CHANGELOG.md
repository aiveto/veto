# Changelog

## Unreleased

## v0.1.9 - 2026-10-07

- A flow with a question is a read-only task. Search and the context pack return it. A later step takes its parameter from a declared relation. Check fails when that field or an answer field is gone, or a step is not a read.
- `veto check --against` names a task that lost a binding or an answer field.
- An invoke failure names its stage in `why`: parameter, policy, catalog, upstream, missing auth, or held until you approve.
- A semantics file that names an operation the catalog does not have fails to load.
- `veto init` writes a read-only task when a contract lists response fields for a read. A declared relation between two reads becomes the two steps.
- `veto propose` drafts a reviewable task from each declared relation. The owner keeps the question and the answer fields that matter.
- `veto run` prints what a read-only task kept true. A missing binding or answer field uses the same sentence as check. The response and its values are not stored. `--save` writes that task. A binding the relation already supplies stays out of the file.
- A flow step that names an operation, response field, or parameter the catalog does not have fails to load.
- A stopped task step names the same stage as invoke. A doctor ping that cannot reach the server says upstream.

## v0.1.8 - 2026-10-06

- A derived page keeps the caller's argument types, so a boolean stays a boolean under policy. A later page failure keeps the first page's HTTP evidence. An integer bound is compared exactly, including additional properties, oneOf, and a structured query. Query and path validation uses the text that is sent. A stopped flow keeps the invoke result. Generated clients reserve `jsonv2` and keep catalog links and page maps, so `next_calls` matches a direct invoke. A JSON Pointer index that overflows is missing.
- A derived page is checked with the same policy arguments as a direct call. Deployment response fields apply after the page walk. A page that was sent and then fails to parse keeps `http` and `sent`.
- The agent loop keeps the invoke result, including send evidence when the transport fails, and puts the response body in the follow-up pack.
- Generated client methods `Confirm`, `Operation`, and `SetPolicy` stay reserved. An integer enum is compared exactly, so a value float64 would collapse is rejected. A context pack omits a pending id that does not fit instead of shortening it.
- The agent loop invokes through the kernel runtime instead of copying its fields. `NewRuntime` takes that runtime. A derived page is validated before it is sent.
- A successful invoke includes `next_calls` when a relation names a path or query value in the response. A missing or projected field is omitted, and a header is not copied. A JSON body integer is compared exactly, so an enum float64 would collapse is rejected.

## v0.1.7 - 2026-10-04

- A confirmation form sends a confirm schema. `veto invoke` asks the same sentence when stdin and stdout are a terminal; `y` runs the call once. A skill or `veto serve --json` still returns the pending id.
- Preview keeps integer digits in the request body. Query, path, and header values are checked against their schema before HTTP. A mismatch is `invalid_param` and does not echo the value. A relation that would only join an operation to itself is refused. Related ids are listed once.

## v0.1.6 - 2026-10-04

- Search returns `related_calls`. A pending id submitted as `approval_id` is `pending_approval` and does not run HTTP. `idempotency_key` is on invoke and on a pin. The agent loop copies the kernel page walk, body cap, and invoke gate. A context pack keeps the pending id after params are dropped. A generated client hides the runtime and exposes `Operation`, `SetPolicy`, and `Confirm`. A grouped tool lists the callable ids. The guide names pins, collisions, pages, SIGTERM, and `sent` versus `http`.
- MCP construction rejects a colliding tool name before any tool is registered. A shared Valkey store claims once through the same State path as Memory and Files.
- Runtime authorizes every pagination page. Exposure tags and paths are AND. Structured query integers stay digits on the HTTP request. A shared Memory store claims once. A Files claim is not deleted when a stale cache is refreshed. `veto serve --json` polls stdin so SIGTERM stops an idle process. Group and pin names cannot replace the three capabilities. A pin omits `operation_id`. Check drift compares every case expectation, and any lost confirmation. The guide names `sent` versus `http` and shared approval storage. ADR 029 supersedes the OR in ADR 022.
- MCP and JSON encode one invoke result. MCP only sets IsError and the confirmation form on that body.
- Approval binds one deterministic JSON form for structured arguments. Large integers stay digits through MCP and JSON. A JSON failure keeps the structured result. Search, serve, preview, and doctor build the catalog without the agent model. `veto serve --json` closes stdin on SIGTERM. Search keeps one synonym snapshot.
- Search, describe, and invoke live in `capability`. Server lives there. MCP and `veto serve --json` call it. `--json` serves JSON lines for skills and scripts. `veto search`, `describe`, and `invoke` are a shorthand. `--help-json` is that contract. `semantics.Notes` is the note and synonym map. The agent and Server use it.
- The README figure sends only invoke through policy and HTTP. Deletes require approval by default.

## v0.1.5 - 2026-10-03

- A loaded schema keeps its validation constraints. Malformed JSON is a body error.
- A response that arrives and then fails still reports that HTTP left.
- Search and synonym maps return caller-owned copies. A store Get names a missing record and a store error separately.
- Distinct approvals do not share one lock across store I/O. A closure can satisfy `credentials.Provider`.
- JSON encode and decode use `encoding/json/v2`. Tokens and approvals stay v2. `json: v1` is `DefaultOptionsV1` for JSON sent to a client API. Response projection skips unused fields with `jsontext`. Retry waits and cancel run in a `synctest` bubble.
- A transport that ran without a response is `sent` and not `http`. A canceled refresh waiter is checked for a goroutine leak.
- Search scores prepared catalog text. Semantics bakes synonyms when it is constructed.
- An HTTP client that already honors the proxy environment is reused. The executor keeps the shaped body instead of reading it again.
- Parameter types and JSON body schemas are prepared with the catalog.
- The first concurrent ByID or Invoke prepares the index and limiter once.
- Invoke says why a call did not run when it was held or missing auth, and whether HTTP left. `caller` is who asked.
- `veto eval` uses the scripted model even when `veto.yaml` names openai.
- The confirmation form names the caller, the operation, and the parameters.
- Auth sources are types. Doctor uses the same source as invoke.
- An empty context pack is an error for every model.

## v0.1.4 - 2026-10-03

- A tagged release pushes `ghcr.io/aiveto/veto` and publishes `io.github.aiveto/veto` to the official MCP registry.

## v0.1.3 - 2026-10-03

- `capabilities_search` returns `id`, the pack call line, related ids, and `confirmation` when the gate is on. Describe still returns the operation.

## v0.1.2 - 2026-10-03

- `brew install aiveto/veto/veto` installs the release binary from the `aiveto/homebrew-veto` tap.
- `memory.New` and `semantics.New` are the constructors. The replica store package is `valkey`.

## v0.1.1 - 2026-10-03

- Over stdio, a host that supports elicitation asks the person to accept a held call. Accept runs it. Decline leaves the pending id for `veto approve`. Over `--http`, `chat_approval: true` turns the form on.
- A contract may be an http or https URL. `veto init` writes that URL into `veto.yaml`.
- The MCP server version comes from the build. A release sets it. A local build reports `dev`.
- `read_only: true` and `expose: {tags, paths}` in `veto.yaml` serve part of a contract. A removed operation leaves search, describe, invoke, and the graph. A bundle cannot set these keys.
- `veto.yaml` drops `decision`, `execution`, `subagents`, and `telemetry`. Each accepted one value. `replay_redact` is a bool.
- Relative paths in `veto.yaml` share one resolver. Approvals, tokens, and the memory log share one atomic file write. `execute.InvokeResponse` takes `Client`. Policy decisions combine through `policy.Combine`.
- Confirmation uses a store and a signer. `SetNonceDir` and `SetSigner` attach the file and HMAC adapters. The CLI holds extracted bundles. A generated client constructs the invoke gate.
- `policy.Store` is public. Memory and Files ship. `valkeystore` is the replica store (Valkey or Redis). `approval_store` names the URL env. `veto approve --config` reads that key and `approval_ttl`. `--pin` registers the pinned tool.
- `policy.Store` and `policy.State` I/O take `context.Context`. `invoke_limit` in `veto.yaml` is calls per second per caller. Unset or 0 is 16.
- A JSON request body is checked against its schema before HTTP. A mismatch returns `invalid_body` with the field path. The value is not echoed.

## v0.1.0 - 2026-10-02

- `trace_export: stdout` uses the same attribute allowlist as `trace_export: otlp`. A response body is not exported.
- A destructive invoke with no confirmation state returns an error and does not call HTTP. `SetSigner` rejects an empty secret. `ErrUnknownApproval` and `ErrInvalidApproval` identify those failures.
- A failed approval webhook still returns the pending id. The error is on the result. Upstream HTTP does not run.
- `approval_ttl` in `veto.yaml` sets the signed approval lifetime. Unset keeps 15 minutes. A bundle cannot set it.
- `GET /healthz` and `GET /readyz` on `veto serve --http` answer 200 without `Veto-Caller`. They report that this process is up.
- MCP tools carry `readOnlyHint` and `destructiveHint`. Search and describe are read-only. Invoke is destructive.
- `confirmation: false` in `veto.yaml` turns the confirmation gate off for every operation in that deployment. Unset leaves it on. `agent.yaml` can still exempt one operation when the key is unset. Doctor and check print `confirmation is off`. A bundle cannot set the key.
- A page walk that stops because the page cap or the byte budget is reached stays `truncated` after field selection. A complete walk stays complete.
- A generated CLI keeps `--help-json`, and `--confirm` on a destructive command. A parameter with one of those names is registered under a different flag, so the command does not panic.
- A generated `go.mod` requires a veto release or the pseudo-version of this commit. It does not require `v0.0.0`.
- The first concurrent invoke on one runtime shares one limiter. A generated client does not race while creating it.
- Generated names avoid the SDK field `Calls`, the CLI locals, and an invalid module path. The required veto version is veto's own module version, not the program that embedded it.
- A second YAML document in `veto.yaml` or a bundle manifest is an error. Alias graphs are counted before they expand.
- A later page that fails does not return the first page as a successful list. Collected pages stay inside the response byte limit.
- `semantics.yaml` rejects an unknown field, a repeated field, a missing operation, a repeated operation, and an alias that loops. `veto.yaml` rejects a repeated field and an alias that loops.
- Scripted replay reads the whole id in `delete order`, so `10abc` stays `10abc`.
- Invoke allows 16 calls in one second in this process, counted per caller. The next call stops before policy, a token URL, and upstream HTTP. That limit stays on.
- A capability bundle is a directory or a zip of contracts, relations, and check cases. `veto check --bundle` and `veto doctor --bundle` load it the same way as a config that points at those files. Credentials, token URLs, client secrets, and environment base URLs stay in the deployment config. A bundle that contains a client secret, a token URL, or a base URL does not load.
- `response_fields`, or `fields` on invoke, returns those JSON fields after a successful call, plus a page and `truncated` when the response cap cuts the body. With no fields named, the body is unchanged. The cap stays. Secrets stay off the trace.
- OPA input includes method, path, side effect, permissions, caller, environment, auth scheme, tags, and resource group, along with operation and params. A missing fact is empty. A deny on method or caller does not call HTTP. Builtin confirmation still applies when Rego allows the call.
- `approval_webhook` names a command or an HTTP URL called when a call is pending. It receives the pending id, the operation, and the caller. It does not receive parameters, secrets, or upstream tokens. `veto approve` remains the local approval. A pending id does not send HTTP.
- `veto serve --http` serves the same three tools over Streamable HTTP. Requests send `Veto-Caller`. That credential is who is calling veto. Traces omit it. Callers do not share approval ids or tokens.
- The container runs `veto --help`. A pushed tag builds the CLI for linux, darwin, and windows on amd64 and arm64. `docs/guide.md` covers `go install` and the orders and customers files in `testdata`.
- `veto doctor` reports missing auth, colliding and fallback ids, parameters it cannot serialize, empty or weak summaries, and writes that require approval. It exits non-zero when a finding would make a call wrong. `veto preview`, and `preview` on `capabilities_invoke`, stop before a token URL and upstream HTTP.
- Upstream auth honors `apiKey` and `oauth2` as well as HTTP bearer. `veto auth login` stores a refresh token. Workforce calls use client credentials. A command can return headers. Policy runs before any token fetch.
- A login can store an auth token and a user token and send them on two headers. `user_header` names the user-token header. `auth_token` and `user_token` rename the JSON fields. A client-credentials token is never that user header.
- `source: token_exchange` posts an RFC 8693 exchange and sends the new access token. Library code implements `credentials.Provider`.
- `veto init` writes `veto.yaml` for the named contracts and a `relations.yaml` stub. An existing `veto.yaml` is left as it is.
- `capabilities_invoke` accepts a JSON object for `params.body` and sends that object. A string body is unchanged.
- A link parameter may be `$response.body#/customer/id`: objects, and one array index. A second index is an error. A link with no parameter mapping is not called.
- Operator notes are in `docs/guide.md`.
- A JSON body is a catalog parameter. An empty required parameter does not call HTTP.
- Bearer auth uses the contract. The secret is an environment variable named in config.
- The context pack shows the selected call and declared relations. `veto pack` prints it.
- `veto eval` and `veto check` run a case file or a directory. `veto doctor` reports pins and missing auth env names.
- `veto replay --from` prints a redacted trace file. `trace_export: otlp` sends that same attribute set.
- `memory: file` is an optional turn log. Unset memory stays in the process.
- `policy: opa` is optional and off unless `policy_file` or `policy_bundle` is set. Builtin permissions and confirmation still apply when Rego allows the call.
- A confirmation result carries a pending id. `veto approve <id>` records the approval. A later invoke with that approved id runs once. The pending id does not send HTTP. With `VETO_APPROVAL_SECRET` the approved id is a signed token. Consume-once is the store: Files, or Valkey or Redis via `approval_store`. Unset, confirmation stays in the process.
- Path-item parameters, path and operation servers, distinct fallback operation ids, and query `style` and `explode` are honored.
- `WrapPolicy` wraps the policy already configured. An extension or a replaced allow-all policy cannot skip confirmation or drop a permission denial.
- `veto check --against` fails when a joined operation disappears, confirmation is dropped without an agent.yaml change, a discovery-only operation becomes callable, a required permission is removed, or an eval expectation changes.
- A discovery-only operation fails at invoke and does not call HTTP.
- HTTP response bodies are capped at 1 MiB.
- Spans and replay do not record parameter values or the user message.
- Follow stops after 8 calls. A declared relation still runs. An OpenAPI link with no parameter mapping does not.
- A 4xx or 5xx reports the failure code from the HTTP call.
- Tool spans set `gen_ai.tool.name` and `gen_ai.operation.name`.
- MCP invoke returns `missing_param` in the same JSON shape as other invoke results.
- An eval case can set `no_http`. The case fails when the call reaches HTTP.
- Generated methods return the invoke result, including a missing parameter.
