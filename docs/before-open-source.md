# Before open source

This is the list of work left before veto is a public project. Pick an item by number. Leave the locked decisions alone.

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
| Multi-API joins | relations file, walk in invoke | 4, 16, 62 |
| Human in the loop | confirmation, interrupt | 33–34, 67 |
| Durable multi-step work | `execution: temporal` key | 40 |
| Real calls | body, auth when needed, retry/idempotency | 1–8, 11–12 |

## Guardrails in veto

**Yes: guardrails on capabilities and actions are veto’s job.** That is the invoke gate, `agent.yaml`, the pack, param checks, confirmation, trace redaction, and CI (`eval`, item 72). **No: veto is not the only guardrail in the stack.** Row-level and token authZ on HTTP stay on the API layer. Content moderation on raw chat may use a `decision:` provider (item 41). External engines for invoke authZ use `policy: opa` or `policy: spicedb` (item 73). Confirmation on destructive calls stays in veto; an external decision provider does not approve a delete by itself.

## Already in the tree

Do not rebuild these.

- One catalog from many OpenAPI files. Duplicate operation ids fail the load. Each contract keeps its own server URL.
- OpenAPI links resolve with a JSON pointer. A dangling link fails the load.
- A relations file joins `schema.field` to an operation, for example `Holding.teamsId` to `teams.get`. A field name alone creates no edge.
- Search, describe, and invoke. Optional pins. Optional one tool per resource. Never one tool per operation.
- Context pack: search hits, their neighbors, rules, the conversation, pending confirmation. The raw spec stays out. A relation becomes a sentence on the note.
- Confirmation is the only policy gate. `agent.yaml` can turn it off, or turn it on, per operation.
- `veto generate` writes a typed SDK, a CLI, and an MCP dispatch. Every call goes through `agent.Invoke`.
- Scripted model by default. `model: openai` reads `OPENAI_API_KEY` and the pack. Evals do not call the network.
- Replay prints an in-memory trace and drops anything off a short allowlist.
- Memory and pending approvals are maps inside the process. They are not a database. That is the default we want.

## Locked

- Interpreter first. `veto serve` runs from the catalog. Generate stays optional.
- Do not guess joins from field names.
- Do not register one MCP tool per operation.
- Do not put the raw OpenAPI document in the context pack.
- Do not import Temporal, Jev, or Ossie. They stay provider keys until a real client exists, and the default must still run with them unset.
- Do not add a database to remember a conversation or an approval. The process is the session. A later item may use a signed token the caller holds. That is not a store.
- `scripted` stays the model default.
- `execute` is the only HTTP writer. Generated code does not build its own request.

## Take these first

These are the heavy lifting a user still does by hand. Ship them in this order. Each one should be config, with the current behavior when the new key is absent.

### 1. Send a JSON body

`execute.Invoke` fills path, query, and header. It does not send a body. A create or update cannot run.

Done when a POST or PUT with a request schema sends `Content-Type: application/json`, a test server receives the body, and a call with no body schema stays as it is now.

### 2. Auth from the contract

The user should not write a client to attach a token. Read the OpenAPI security scheme. A config key names the env var. `execute` sets the header. The key never goes in yaml, and it never goes on a span.

Done when a bearer scheme on the contract sends `Authorization` from the env var, a test proves the header is absent from replay, and a contract with no scheme sends no auth header.

### 3. Put the selected call shape in the pack

The pack names operations and relation sentences. It does not say which parameters are required, or what the response fields are. The model still has to guess or call describe.

Done when a hit in the pack includes parameter name, location, and required flag, plus the response fields needed to follow a relation (for example `teamsId` on `Holding`). The raw document is still absent. A test with an unrelated operation still drops it.

### 4. Walk a declared relation

The sentence `Holding.teamsId identifies teams.get` is text. The user still writes a flow, or the model makes two calls and copies the id.

Done when one invoke can follow that edge: call the first operation, read the field, call the target with that value. Both calls use `agent.Invoke`. A missing field is an error. No new edge appears unless the relation file or an OpenAPI link declared it.

### 5. Pass step output into the next flow step

`flow` runs a list of operation ids and passes the same params to each one. The body of step one is thrown away.

Done when a flow step can name an output field and the next step's parameter. Confirmation still stops a destructive step. The linear runner stays. No graph engine.

### 6. Make `idempotency` and `retry` do something

`agent.yaml` stores `idempotency` and `retry`. Execute ignores them.

Done when `idempotency: key` sends an `Idempotency-Key` header, `retry` retries only idempotent calls on 429 and 5xx with a small bound, and `retry: never` (the sample delete) does not retry. A test locks that a destructive call without an idempotency key is not retried.

### 7. Point `model: openai` at any compatible host

The provider always uses `https://api.openai.com/v1` unless tests pass a base URL in code. Config cannot name Azure, a proxy, or a local server.

Done when `model_base_url` in `veto.yaml` is the host, the key stays in the environment, an empty pack or an empty key is still an error, and eval still uses `scripted`.

### 8. Generated clients keep each server URL

`veto generate` builds one client. The CLI falls back to `http://127.0.0.1:8080`. A catalog of two APIs loses the second server.

Done when each generated method uses the operation base URL from the catalog, and `VETO_BASE_URL` still overrides all of them when set.

## Calls

9. Validate required path, query, and header params before HTTP. An empty path param must not produce a broken URL.
10. Coerce model params. The OpenAI reply is `map[string]string`. A number or a nested body should not fail the parse. Scalars become strings for path and query. Objects stay for the body in item 1.
11. Timeouts. One config timeout for the HTTP client. A hung call ends. The default stays long enough for tests.
12. Stable errors. Return status, a short code, and whether the call is retryable. The model sees that, not only a raw body. Replay records `http.status` and does not record the body unless `--keep-sensitive`.
13. Pagination. If a list response links the next page, a config flag `page: follow` collects pages up to a cap. Default is one page, so today's list calls stay one request.
14. Multiple servers on one contract. Today the first server URL wins. Document that, then support a named server when the contract has more than one.

## Relations and catalog

15. `veto validate` prints the joins: operation, edge note, target. A human can see `Holding.teamsId identifies teams.get` without reading spans.
16. Execute OpenAPI links the same way as item 4. The link already names the target. Use the link parameter mapping from the spec, not a guessed field.
17. Shared schema `$ref` stays a `uses` edge. It is not a call edge. Do not turn "both operations mention Holding" into an invoke.
18. Fail load on a relation whose schema is unused or whose target operation is missing. This already happens. Keep the test.
19. Keep the test that `teamsId` with no relations file does not connect to `teams.get`.
20. OpenAPI 3.1, callbacks, and webhooks. Write down what the loader accepts and what it rejects. Do not silently drop a callback and claim the catalog is complete.
21. Protobuf stays out until OpenAPI is boring. ADR 006. No empty `protobuf` package.

## Context pack and semantics

22. Truncate the pack on an operation boundary. Today a short budget can cut an id in half.
23. Cap search hits. Prefer an exact operation id, then a synonym, then a neighbor. A large spec must not fill the pack with weak matches.
24. Recent turns. Memory search only returns an old line when the new message is a substring of it. Keep the last N turns of this process in the pack. Still no database.
25. Tags and path nouns. The signed note says synonyms come from tags and the path noun. The code uses description, name, id, and a small builtin map. Either implement the claim or correct the note. Do not leave them different.
26. Ossie 0.1.1 reader behind `semantics: ossie`. Derived notes stay the default. Do not import Ossie 0.2 drafts. An overlay must still keep the relation sentence.
27. Describe and the pack stay one story. The describe payload for an operation includes the same params, response fields, and relation sentence the pack would.

## MCP

28. `capabilities_invoke` returns `confirmation_required` and the approval id in a shape the client can send back without reading Go types.
29. Document exposure: `direct`, `grouped`, `discovery-only`. A discovery-only pin is an error. Keep that.
30. Grouped tools stay one tool per resource. Add a test that a 50-operation spec does not register 50 tools.
31. Search paging when the caller asks for more hits. Default page stays small.
32. Stdio is the transport for the first public release. An HTTP transport is later, and only if a host cannot speak stdio.

## Policy and confirmation

33. Show the call before it runs. The confirmation the human sees names the operation and the params. The same sentence is in the pack.
34. Signed approval, only if we want a reconnect to work. HMAC of operation, params, and expiry, secret from the environment. The caller holds the token. The server stores nothing. Process maps remain the default if this item is skipped. Document which one shipped.
35. Permissions. `agent.yaml` lists them and the builtin hook allows every one. Add a caller identity and deny a call whose permission is missing. The default identity allows the sample catalog, so eval does not start failing.
36. Allow and deny stay inside `policy.Hook`. No second gate.

## Models, memory, execution

37. No vendor SDK. Another model is the same HTTP shape as item 7, or it waits.
38. `memory: local` stays the default. Do not add Postgres, Redis, or a vector store for the public release.
39. Optional `memory: file` is allowed only as an off-by-default key, after item 24. The file is a log of turns, not a product.
40. `execution: temporal` is a provider key. The default stays `in-process`. The Temporal client is not imported until a flow can run as a workflow and the in-process runner still passes tests with the key unset.
41. `decision: jev` is a provider key. Jev does not approve a delete. Confirmation stays in veto. No Jev client until the hook exists and the default decision still confirms a delete.
42. `subagents: off` stays the only accepted value until there is a real design. Reject any other value, as now.

## Replay and traces

43. Say what replay is. It runs the message now and prints the trace. It does not open yesterday's run. Put that in the README in one sentence, or add a reader for a trace file. Do not imply a history store.
44. `trace_export: otlp` as an optional key. Empty export stays a noop. `stdout` stays. Secrets stay off the allowlist.
45. New span attributes are omitted by replay until someone adds them to the allowlist on purpose. Add a test if you add a key.
46. The API key, the Authorization header, and the user message stay off the default replay. `--keep-sensitive` and `replay_redact: false` print them.

## Generate, eval, examples

47. Generated methods take a body argument when the operation has a request schema, after item 1.
48. Generated `--help-json` lists params, confirmation, permissions, and the server URL.
49. A second eval case: two contracts, a relation, and a user sentence that must select the neighbor and must not select an unrelated operation.
50. A third eval case: delete is refused without approval, and the test server receives the delete only on the second call. The sample case already covers the refusal. Keep a server assertion in the unit test.
51. `examples/assets` is one file. Add a second example that is the assets plus teams config, with the commands from the README, so a new person can run it.

## Public release hygiene

52. Choose a license. No public repo without one.
53. `CONTRIBUTING.md` with the Go rules already in `CLAUDE.md`: gofmt, errors, no `util` package, tests that lock behavior.
54. Security policy: where to report a bug that leaks a token or skips confirmation.
55. CI runs `gofmt` and `go test ./...` on a pull request.
56. Module path stays `github.com/aiveto/veto`. A tagged version before anyone is asked to import it.
57. Changelog for the first tag. User-facing only.
58. README matches the binary. Commands, the config file, confirmation, the pack, replay, and a pointer here. No tour.
59. Scan for secrets before the first public push. Keys live in the environment.
60. The GitHub repo stays private until items 1, 2, 3, 52, 54, and 55 are done. Those are the ones that make a public demo honest: a real call, auth, a pack the model can fill, a license, a way to report a hole, and tests on every change.

## Loop

61. The loop is one model call. It returns a follow-up pack to the caller and does not call the model again with the tool result. A later loop may do that. The scripted eval must stay one decision, so a delete still stops on confirmation instead of calling HTTP.

## Wow slice

Ship items 1 through 8 first. Nothing below replaces them. Flat OpenAPI-to-MCP generators already exist. Veto wins when several contracts, declared joins, a small MCP surface, a bounded pack, and one invoke gate are real in one config.

**Pitch for open source (when item 60 is done):** Register your OpenAPI files, declare joins, run MCP. Veto merges catalogs, builds the pack, gates every call, and lets you test the agent config in CI.

Philosophies in `CLAUDE.md` point here without importing their code: Eino-style explicit steps and interrupts; Kong, Stainless, and FastMCP-style search then describe then invoke; OpenTelemetry for prove and replay; provider keys for Temporal, Jev, and Ossie later.

### P0 (must-use differentiators)

62. **Executable catalog.** Item 4 walks a declared relation in code, not only as a sentence in the pack. One user intent can call `assets.get`, read `teamsId`, then call `teams.get`. Every hop uses `agent.Invoke`. The trace shows each operation. OpenAPI links use the spec mapping when item 16 lands.

63. **`veto pack`.** A command prints the pack for a message: `veto pack --config veto.yaml --message "..."`. A `--json` flag for CI. Assert the index contains expected operation ids, relation sentences, and neighbors, and does not contain unrelated operations. Same builder as `serve` and the live model.

64. **Eval suite as product.** A directory of case yaml files and `veto eval ./cases/` (or repeat `--case`). Cases assert operation choice, confirmation, no HTTP when expected, pack substrings, and related ids. Document one standard layout under `testdata/` or `examples/`. CI runs eval when contracts or `relations.yaml` change.

### P1 (trust and onboarding)

65. **`veto validate` explains the graph.** After load, print each join as a line, for example `assets.get --[Holding.teamsId]--> teams.get`. Item 15 overlaps; keep one implementation. Optional later: warn when a contract version removes an operation a relation or link still references.

66. **`veto doctor`.** Before `serve`, check contracts load, relations are consistent, required env vars for security schemes are set (names only, never values), pins are not discovery-only, and optional `--ping` reaches server URLs. One stderr report, exit non-zero on blockers.

67. **Interrupt and resume on confirmation.** Generalize confirmation to an Eino-style interrupt: pending state ties to the trace (approval id or resume token). MCP `capabilities_invoke` documents the round trip. Process memory stays the default; a signed token the caller holds is optional later (item 34).

68. **Stable invoke errors to the model and MCP.** Beyond item 12: JSON or structured text with `code`, `retryable`, `missing_param`, `confirmation_required`. Same shape from MCP invoke and generated SDK. Replay keeps the allowlist; do not log secrets.

### P2 (ops and semantics)

69. **Replay from exported traces.** Item 43 and 44: optional write of redacted spans to a file; `veto replay --from trace.json`. OTLP export as a config key. Default replay stays run-now, in memory.

70. **Semantics without Ossie in v1.** Item 25 and 26: tags and path nouns in derived notes; relation sentences on notes; file overlay optional. `semantics: ossie` waits on a reader, not on Ossie 0.2 drafts.

71. **Linear flows with step I/O.** Item 5: a step names an output field and the next step's parameter. Confirmation still stops a destructive step mid-flow. No graph engine (ADR 010).

## Enterprise rollout

Backend token validation and outbound auth often live on the API gateway or mesh. Veto does not replace that. `execute` may call a gateway base URL with no `Authorization` when the gateway attaches identity. Item 2 stays for teams that call APIs directly. Item 35 still needs a **caller identity** for invoke policy (who may ask for a delete), which is not the same as the gateway JWT to a microservice.

### Killer not on the gateway

72. **`veto check` for agent surface regression.** One CI command after items 64 and 65 exist: load `veto.yaml`, relations, and agent metadata; validate the graph; run the eval case directory; fail non-zero on any error. Optional `--against` a git ref or a committed snapshot: fail when an OpenAPI change removes an operation referenced by a relation or link, when a destructive operation loses `RequiresConfirmation` without an intentional `agent.yaml` change, or when an eval case changes expected operation or confirmation behavior. Gateways validate HTTP requests. They do not know whether `Holding.teamsId` still reaches `teams.get` or whether "delete asset 123" still stops before HTTP. Platform teams need that gate when API repos and agent config ship on different PRs.

73. **External invoke policy (`policy: opa` or `policy: spicedb`).** After item 35. Config key only until a provider package exists. Subject comes from caller identity (item 35) or MCP session metadata. Check `op.Permissions` and later resource ids from params against OPA or SpiceDB. Default stays `policy: builtin`. Eval and `veto check` must still pass with the key unset. This is agent-surface authZ, not replacement for gateway authZ on HTTP.

### Do not chase for "wow"

These do not belong in the must-use story:

- One MCP tool per operation (ADR 003).
- Guessed foreign keys from field names.
- Default Postgres, Redis, or a vector store for memory.
- Subagents and LangGraph-style orchestration in the first public release.
- Competing with Stainless on prettiest SDK alone. Generate stays typed and policy-gated.
- Protobuf parity before OpenAPI is boring (ADR 006).

## Not this release

- A database for sessions or approvals.
- One MCP tool per operation.
- Guessed foreign keys.
- A graph workflow engine. Flows stay a list.
- Code mode, subagents, and a vector store.
- Replacing the scripted model in eval.
