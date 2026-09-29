# Before open source

Each numbered item is **Done**, **Open**, **Waiting**, or **Deferred**. Done means this tree does it, through `f879306`. Waiting stays until a public launch. Deferred is not work to build.

## Still worth doing

None. The three MCP tools, OpenAPI links, `veto check`, and replay from a trace file are already in the tree.

Numbered items **1 through 73**, the **Locked** section, and **Must-have map** below are the product checklist. Add or reorder work only with a short ADR, then update this file.

The product is one catalog for every API a user registers. They write config. Veto does the plumbing: a small MCP surface, a context pack that follows declared joins, guardrails at invoke, and the same gate for the SDK, the CLI, and MCP. A flat OpenAPI-to-SDK tool is not the thing we are shipping.

## Must-have map (locked)

How common agent and MCP concerns map to veto. API gateways still own **data-plane** authZ on HTTP. Veto owns the **agent surface** and **capability guardrails**.

| Concern | Veto owns | Items / ADR |
|--------|-----------|-------------|
| MCP tool surface (not one tool per operation) | search, describe, invoke, pins, grouped | ADR 003, 28–30 |
| Context for the model (not dumping the spec) | pack selection, budget | 3, 22–23, 63 |
| Semantics / capability descriptions | derived notes, overlay, relation sentences | 25–27, 70 |
| Guardrails: which ops exist for this deployment | exposure, discovery-only, pins | `agent.yaml`, 28–29 |
| Guardrails: may this invoke run | `policy.Hook`, confirmation | 33–36, 67–68, 73 |
| Guardrails: safe params | validate and coerce before HTTP | 9–10 |
| Guardrails: leaks in traces | replay allowlist | 43–46, 69 |
| Guardrails: free-text / prompt (optional) | `decision:` provider, not core | 41 |
| AuthZ to backend APIs | gateway or mesh (primary); direct auth optional | Enterprise rollout; item 2 when no gateway |
| AuthZ on the agent surface | caller + permissions on invoke | 35, 73 |
| Observability | OpenTelemetry, replay | 43–46, 69 |
| Test agent config in CI | eval cases, `veto check` | 49–50, 64, 72 |
| Multi-API joins | OpenAPI links, else the relations file | 4, 16, 62 |
| Human in the loop | confirmation, interrupt | 33–34, 67 |
| Durable multi-step work | `execution: temporal` key | 40 |
| Real calls | body, auth when needed, retry/idempotency | 1–8, 11–12 |

## Guardrails in veto

**Yes: guardrails on capabilities and actions are veto’s job.** That is the invoke gate, `agent.yaml`, the pack, param checks, confirmation, trace redaction, and CI (`eval`, item 72). **No: veto is not the only guardrail in the stack.** Row-level and token authZ on HTTP stay on the API layer. Content moderation on raw chat may use a `decision:` provider (item 41). `policy: opa` and `policy: spicedb` fail closed until a provider exists (item 73). Confirmation on destructive calls stays in veto; an external decision provider does not approve a delete by itself.

## Already in the tree

Do not rebuild these.

- One catalog from many OpenAPI files. Duplicate operation ids fail the load. Each contract keeps its own server URL.
- OpenAPI links resolve with a JSON pointer. A dangling link fails the load.
- A relations file joins `schema.field` to an operation, for example `Order.customerId` to `customers.get`. A field name alone creates no edge.
- Search, describe, and invoke. Optional pins. Optional one tool per resource. Never one tool per operation.
- Context pack: search hits, their neighbors, rules, the conversation, pending confirmation. The raw spec stays out. A relation becomes a sentence on the note.
- Confirmation stops a destructive call. A missing permission denies inside the same hook. `agent.yaml` can turn confirmation off, or turn it on, per operation.
- `veto generate` writes a typed SDK, a CLI, and an MCP dispatch. Every call goes through `agent.Invoke`.
- Scripted model by default. `model: openai` reads `OPENAI_API_KEY` and the pack. Evals do not call the network.
- Replay prints a trace and can read a trace file back. It drops anything off a short allowlist.
- Memory and pending approvals are maps inside the process. They are not a database. That is the default we want. Do not add Mem0, a vector database, or a new memory store. In-process memory and the trace file stay.

## Locked

- Interpreter first. `veto serve` runs from the catalog. Generate stays optional.
- Do not guess joins from field names.
- Do not register one MCP tool per operation.
- Do not put the raw OpenAPI document in the context pack.
- Do not import Temporal or Jev. They stay provider keys until a real client exists, and the default must still run with them unset.
- Apache Ossie is deferred. It is a warehouse metrics standard, not veto's HTTP link file.
- Do not add a database to remember a conversation or an approval. The process is the session. A signed token the caller holds is optional. That is not a store. Do not add Mem0, a vector database, or a new memory store. In-process memory and the trace file stay.
- `scripted` stays the model default.
- `execute` is the only HTTP writer. Generated code does not build its own request.

## Take these first

These were the calls a user had to write by hand. Each one is config, and the old behavior stays when the new key is absent.

### 1. **Done.** Send a JSON body

A POST or PUT with a request schema sends the body. `Content-Type` comes from the contract, or `application/json` when the contract is silent. A call with no body schema sends no body.

### 2. **Done.** Auth from the contract

A bearer scheme sends `Authorization` from the env var named in config. The secret is not in yaml and not on a span. A contract with no scheme sends no auth header.

### 3. **Done.** Put the selected call shape in the pack

A hit in the pack includes parameter name, location, and required flag, plus the response field needed to follow a relation (for example `customerId` on `Order`). The raw document stays out. An unrelated operation stays out.

### 4. **Done.** Walk a declared relation

One invoke follows a declared edge: call the first operation, read the field, call the target with that value. Both calls use `agent.Invoke`. A missing field is an error. No new edge appears unless the relation file or an OpenAPI link declared it.

### 5. **Done.** Pass step output into the next flow step

A flow step can name an output field and the next step's parameter. Confirmation still stops a destructive step. The linear runner stays. No graph engine.

### 6. **Done.** Make `idempotency` and `retry` do something

`idempotency: key` sends an `Idempotency-Key` header. `retry` retries only idempotent calls on 429 and 5xx, with a small bound. `retry: never` does not retry. A destructive call without an idempotency key is not retried.

### 7. **Done.** Point `model: openai` at any compatible host

`model_base_url` in `veto.yaml` is the host. An empty value uses `https://api.openai.com/v1`. The key stays in the environment. An empty pack or an empty key is an error. Eval still uses `scripted`.

### 8. **Done.** Generated clients keep each server URL

Each generated method uses the operation base URL from the catalog. `VETO_BASE_URL` still overrides all of them when set.

## Calls

9. **Done.** Validate required path, query, and header params before HTTP. An empty path param must not produce a broken URL.
10. **Done.** Coerce model params. The OpenAI reply is `map[string]string`. A number or a nested body should not fail the parse. Scalars become strings for path and query. Objects stay for the body in item 1.
11. **Done.** Timeouts. One config timeout for the HTTP client. A hung call ends. The default stays long enough for tests.
12. **Done.** Stable errors. Return status, a short code, and whether the call is retryable. The model sees that, not only a raw body. Replay records `http.status` and does not record the body unless `--keep-sensitive`.
13. **Done.** Pagination. If a list response links the next page, a config flag `page: follow` collects pages up to a cap. Default is one page, so today's list calls stay one request.
14. **Done.** Multiple servers on one contract. The first server URL wins until `server` names another one.

## Relations and catalog

15. **Done.** `veto validate` prints the joins: operation, edge note, target. A human can see `Order.customerId identifies customers.get` without reading spans.
16. **Done.** Execute OpenAPI links the same way as item 4. The link already names the target. Use the link parameter mapping from the spec, not a guessed field. The relations file is only for a join the spec does not declare.
17. **Done.** Shared schema `$ref` stays a `uses` edge. It is not a call edge. Do not turn "both operations mention Order" into an invoke.
18. **Done.** Fail load on a relation whose schema is unused or whose target operation is missing. This already happens. Keep the test.
19. **Done.** Keep the test that `customerId` with no relations file does not connect to `customers.get`.
20. **Done.** The loader accepts OpenAPI 3.0 and 3.1. Callbacks and webhooks fail the load. They are not dropped.
21. **Done.** Protobuf stays out until OpenAPI is boring. ADR 006. No empty `protobuf` package.

## Context pack and semantics

22. **Done.** Truncate the pack on an operation boundary. A short budget drops a whole operation line. It does not cut an id in half.
23. **Done.** Cap search hits. Prefer an exact operation id, then a synonym, then a neighbor. A large spec must not fill the pack with weak matches.
24. **Done.** Recent turns. Keep the last N turns of this process in the pack. Still no database.
25. **Done.** Tags and the path noun are in the derived synonyms, with description, name, id, and the builtin map.
26. **Deferred.** Apache Ossie is a warehouse metrics standard. It is not veto's HTTP link file. Do not build a reader.
27. **Done.** Describe and the pack stay one story. The describe payload for an operation includes the same params, response fields, and relation sentence the pack would.

## MCP

28. **Done.** `capabilities_invoke` returns `confirmation_required` and the approval id in a shape the client can send back without reading Go types.
29. **Done.** Exposure is `direct`, `grouped`, or `discovery-only`. A discovery-only pin is an error.
30. **Open.** Grouped tools stay one tool per resource. A test that a 50-operation spec does not register 50 tools is not in the tree.
31. **Done.** Search paging when the caller asks for more hits. Default page stays small.
32. **Done.** Stdio is the transport. `veto serve` listens on stdio. An HTTP transport is later, and only if a host cannot speak stdio.

## Policy and confirmation

33. **Done.** Show the call before it runs. The confirmation the human sees names the operation and the params. The same sentence is in the pack.
34. **Done.** Signed approval. HMAC of operation, params, and expiry, secret from `VETO_APPROVAL_SECRET`. The caller holds the token. The server stores nothing. With the secret unset, process maps remain the default.
35. **Done.** Permissions. `agent.yaml` lists them. A caller identity can deny a call whose permission is missing. The default identity allows the sample catalog, so eval does not start failing.
36. **Done.** Allow and deny stay inside `policy.Hook`. No second gate.

## Models, memory, execution

37. **Done.** No vendor SDK. Another model is the same HTTP shape as item 7, or it waits.
38. **Done.** `memory: local` stays the default. Do not add Mem0, Postgres, Redis, a vector database, or a new memory store. In-process memory and the trace file stay.
39. **Done.** Optional `memory: file` is an off-by-default key. The file is a log of turns, not a product.
40. **Done.** `execution: temporal` is a provider key and fails closed. The default stays `in-process`. The Temporal client is not imported.
41. **Done.** `decision: jev` is a provider key and fails closed. Jev does not approve a delete. Confirmation stays in veto. No Jev client.
42. **Done.** `subagents: off` stays the only accepted value. Any other value is rejected.

## Replay and traces

43. **Done.** Replay can run the message now and print the trace. `veto replay --from` reads a trace file back. That file is not a history store.
44. **Done.** `trace_export: otlp` is an optional key. Empty export stays a noop. `stdout` stays. Secrets stay off the allowlist.
45. **Done.** New span attributes are omitted by replay until someone adds them to the allowlist on purpose. A test locks that.
46. **Done.** The API key, the Authorization header, and the user message stay off the default replay. `--keep-sensitive` and `replay_redact: false` print them.

## Generate, eval, examples

47. **Done.** Generated methods take a body argument when the operation has a request schema.
48. **Done.** Generated `--help-json` lists params, confirmation, permissions, and the server URL.
49. **Done.** A second eval case: two contracts, a relation, and a user sentence that must select the neighbor and must not select an unrelated operation.
50. **Done.** A third eval case: delete is refused without approval, and the test server receives the delete only on the second call.
51. **Done.** `examples/two-apis` is the orders plus customers config, with the commands in the README.

## Public release hygiene

52. **Done.** Choose a license. No public repo without one.
53. **Done.** `CONTRIBUTING.md` with the Go rules already in `CLAUDE.md`: gofmt, errors, no `util` package, tests that lock behavior.
54. **Done.** Security policy: where to report a bug that leaks a token or skips confirmation.
55. **Done.** CI runs `gofmt` and `go test ./...` on a pull request.
56. **Waiting.** The module path stays `github.com/aiveto/veto`. A version tag waits until a public launch.
57. **Done.** Changelog. User-facing only. The version tag is item 56.
58. **Open.** The README does not yet match the binary: commands, the config file, confirmation, the pack, replay, and a pointer here. No tour.
59. **Waiting.** A secret scan waits until a public launch. Keys live in the environment.
60. **Waiting.** Making the repository public waits until a public launch. Do not change visibility as build work.

## Loop

61. **Done.** The loop is one model call. It returns a follow-up pack to the caller and does not call the model again with the tool result. A later loop may do that. The scripted eval must stay one decision, so a delete still stops on confirmation instead of calling HTTP.

## Wow slice

Items 1 through 8 are done. Flat OpenAPI-to-MCP generators already exist. Veto wins when several contracts, declared joins, a small MCP surface, a bounded pack, and one invoke gate are real in one config.

**Pitch for open source (when item 60 is no longer waiting):** Register your OpenAPI files, declare joins, run MCP. Veto merges catalogs, builds the pack, gates every call, and lets you test the agent config in CI.

Philosophies in `CLAUDE.md` point here without importing their code: Eino-style explicit steps and interrupts; Kong, Stainless, and FastMCP-style search then describe then invoke; OpenTelemetry for prove and replay; provider keys for Temporal and Jev later. Apache Ossie is deferred.

### P0 (must-use differentiators)

62. **Done. Executable catalog.** Item 4 walks a declared relation in code, not only as a sentence in the pack. One user intent can call `orders.get`, read `customerId`, then call `customers.get`. Every hop uses `agent.Invoke`. The trace shows each operation. OpenAPI links use the spec mapping.

63. **Done. `veto pack`.** A command prints the pack for a message: `veto pack --config veto.yaml --message "..."`. A `--json` flag for CI. Assert the index contains expected operation ids, relation sentences, and neighbors, and does not contain unrelated operations. Same builder as `serve` and the live model.

64. **Done. Eval suite as product.** Case files live in `testdata/cases`. `veto eval` takes a file or a directory. Cases assert operation choice, confirmation, no HTTP when expected, pack substrings, and related ids. CI runs `veto check` on that directory.

### P1 (trust and onboarding)

65. **Done. `veto validate` explains the graph.** After load, print each join as a line, for example `orders.get --[Order.customerId]--> customers.get`. Item 15 overlaps; keep one implementation. A removed join fails in `veto check --against` (item 72).

66. **Done. `veto doctor`.** Before `serve`, check contracts load, relations are consistent, required env vars for security schemes are set (names only, never values), pins are not discovery-only, and optional `--ping` reaches server URLs. One stderr report, exit non-zero on blockers.

67. **Done. Interrupt and resume on confirmation.** Pending state ties to the trace by approval id. MCP `capabilities_invoke` documents the round trip. Process memory stays the default. A signed token the caller holds is item 34.

68. **Done. Stable invoke errors to the model and MCP.** JSON with `code`, `retryable`, `missing_param`, `confirmation_required`. Same shape from MCP invoke and the generated SDK. Replay keeps the allowlist; do not log secrets.

### P2 (ops and semantics)

69. **Done. Replay from exported traces.** Optional write of redacted spans to a file; `veto replay --from trace.json`. OTLP export is a config key. Default replay stays run-now, in memory.

70. **Done. Semantics without an Ossie reader.** Tags and path nouns are in derived notes. Relation sentences stay on notes. A file overlay is optional. Apache Ossie is deferred (item 26).

71. **Done. Linear flows with step I/O.** Item 5: a step names an output field and the next step's parameter. Confirmation still stops a destructive step mid-flow. No graph engine (ADR 010).

## Enterprise rollout

Backend token validation and outbound auth often live on the API gateway or mesh. Veto does not replace that. `execute` may call a gateway base URL with no `Authorization` when the gateway attaches identity. Item 2 stays for teams that call APIs directly. Item 35 is a caller identity for invoke policy (who may ask for a delete), which is not the same as the gateway JWT to a microservice.

### Killer not on the gateway

72. **Done. `veto check` for agent surface regression.** Load `veto.yaml`, relations, and agent metadata; print the joins; run the eval case directory; fail non-zero on any error. `--against` a git ref or a committed snapshot fails when an OpenAPI change removes an operation referenced by a relation or link, when a destructive operation loses confirmation without an intentional `agent.yaml` change, or when an eval case changes expected operation or confirmation behavior.

73. **Open. External invoke policy (`policy: opa` or `policy: spicedb`).** The keys exist and fail closed. They do not check `op.Permissions`. Default stays `policy: builtin`. Eval and `veto check` still pass with the key unset. This is agent-surface authZ, not replacement for gateway authZ on HTTP.

### Do not chase for "wow"

These do not belong in the must-use story:

- One MCP tool per operation (ADR 003).
- Guessed foreign keys from field names.
- Default Postgres, Redis, Mem0, a vector database, or a new memory store. In-process memory and the trace file stay.
- Subagents and LangGraph-style orchestration in the first public release.
- Competing with Stainless on prettiest SDK alone. Generate stays typed and policy-gated.
- Protobuf parity before OpenAPI is boring (ADR 006).

## Not this release

- A database for sessions or approvals.
- One MCP tool per operation.
- Guessed foreign keys.
- A graph workflow engine. Flows stay a list.
- Code mode, subagents, Mem0, a vector database, or a new memory store. In-process memory and the trace file stay.
- Replacing the scripted model in eval.
