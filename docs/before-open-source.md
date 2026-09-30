# Before open source

## Done

One catalog for every API you register. MCP exposes search, describe, and invoke. The context pack is bounded and does not contain the raw spec. A declared relation (`schema`, `field`, `to:`) is the next call. Policy runs before HTTP. A destructive call sends no HTTP until confirmation is stored. Replay and evals run on that path. `veto generate` writes a Go client that calls through the same gate. It is not a typed SDK. `veto check --against` fails when a joined operation disappears or confirmation is dropped without an agent.yaml change. Apache Ossie is not in the product.

Also already in the tree: bearer auth and JSON bodies from the contract, one page unless `page: follow`, linear flows, `veto doctor`, `veto pack`, and in-process memory. OpenAPI links that name a response field run the same way as a declared relation. A link parameter may walk `$response.body#/` through objects and one array index. A link with no parameter mapping is not a call. Unset confirmation stays in the process.

## This release

1. Discovery-only exposure fails at invoke. `agent.Invoke` is the admission check. `capabilities_invoke` uses that path.
2. A signed approval is one use. The token covers the operation, params, expiry, and a nonce. A consumed nonce is kept on this machine and is not accepted again.
3. HTTP responses are capped at 1 MiB. A larger body is an error the caller can see.
4. Spans do not record parameter values or the user message. Parameter names can be recorded. Replay does not receive a user message to strip.
5. Follow stops after 8 calls. A declared relation still runs. An OpenAPI link with no parameter mapping does not invent a call. Link execution reads a response field, or a JSON pointer with objects and one array index.
6. A 4xx or 5xx surfaces the failure code execute already returns. The call is not reported as success.
7. A response pointer may name an object path and one array index, as in `$response.body#/customer/id`. A second index is rejected.
8. `policy: opa` is optional and off by default. `policy.Hook` stays the gate. Rego lives in its own package and is wired only when config says `opa`, with `policy_file` or `policy_bundle`. Builtin permissions and confirmation still run when Rego allows the call. Input is the operation id, params, environment, and principal when present. The decision is allow, deny, or confirmation, plus a short reason.
9. `veto check --against` also fails when a discovery-only operation becomes callable, or a required permission is removed. The check reads the catalog. It does not use oasdiff.
10. Existing tool spans set `gen_ai.tool.name` and `gen_ai.operation.name` to the operation id.

## Post-MVP

Pick up anytime, in this order.

- CEL, first. Light policy expressions for people who do not want a Rego bundle. Builtin stays the default. OPA stays the enterprise engine. CEL is a third engine on the same hook, not a replacement and not a YAML policy language of our own. It waits until this release's hook is in.
- Response shaping: return only named fields, on top of the byte cap.
- Real JSON schemas for MCP arguments, not only strings.
- `veto serve --http` for a remote agent, with the caller identity passed into policy.
- Richer agent-surface diff after the three catalog checks: response schema, server, and weakened auth. oasdiff may feed raw OpenAPI breaking changes into that check. oasdiff is not the product.
- OpenFGA as another optional adapter on the same hook. Not core. Not this release.

## Not in veto

Plan type, `veto plan`, `veto explain`, a parameter-policy DSL, splitting a declared relation into a hint plus a link, a typed SDK, a comment on every export, vector DB, a memory framework, subagents, Temporal in core, OpenInference, Ossie. Version tag, secret scan, and a public repo wait for launch.
