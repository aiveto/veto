# Changelog

## Unreleased

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
- A confirmation result carries a pending id. `veto approve <id>` records the approval. A later invoke with that approved id runs once. The pending id does not send HTTP. With `VETO_APPROVAL_SECRET` the approved id is a signed token, and a consumed nonce is kept on this machine and is not accepted again. `VETO_APPROVAL_NONCE_DIR` overrides that directory. Unset, confirmation stays in the process.
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
