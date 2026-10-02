# veto

Module `github.com/aiveto/veto`. Command `veto`. Repository `github.com/aiveto/veto`.

The name you say is veto. aiveto is the org.

## Product

Point at an OpenAPI contract. Serve a small MCP surface. Build a context pack. Apply semantics. Refuse a destructive call until confirmation is stored. SDK, CLI, and MCP read one catalog. The model may request a call. The runtime decides.

## Philosophies

Take these. Do not import their code.

- Eino: explicit steps, callbacks as spans, an interrupt is state you can resume.
- A good default, replaced by one key. Unset means the default works.
- Kong, Stainless, FastMCP: do not register one MCP tool per operation. Search, then describe, then invoke.
- OpenTelemetry for traces. kin-openapi to load OpenAPI. `modelcontextprotocol/go-sdk` for MCP. Cobra for the `veto` binary. yaml.v3 for overlays.
- Go: Dave Cheney and Mat Ryer. The rules are below. No package named `context`. The pack is `runctx`.

## Layout

```
cmd/veto/
catalog/     Catalog, Operation, graph, search
agentmeta/   agent.yaml overlay on the catalog
openapi/     Loader to *catalog.Catalog
semantics/   Provider. Derived notes, file overlay
runctx/      Context pack. Budget, select, truncate
policy/      Allow, check, confirm. Run state
flow/        Sequential steps. The model may pick the flow. Code runs it
agent/       One turn. Completer, scripted default, policy, execute. The follow-up pack goes to the caller
agent/openai OpenAI HTTP client for the openai provider key
generate/    Typed SDK and CLI. Dispatch calls runtime.Invoke
execute/     HTTP from an Operation
memory/      Local map. The Memory interface is agent.Memory
config/      Provider keys. One file, defaults when unset
auth/        Upstream credentials. Login, client credentials, token exchange, command
credentials/ Provider an embedder implements for one upstream call
bundle/      Contracts, relations, and check cases. Deployment stays outside
opa/         Rego policy. The builtin permission floor still runs
result/      HTTP result and parameter errors
runtime/     Invoke sequence for the CLI, MCP, and generated clients
mcpserver/   capabilities_search, capabilities_describe, capabilities_invoke, pins
eval/        Cases on the agent loop. No network LLM
telemetry/   OpenTelemetry spans. Stdout export is optional
replay/      Read those spans. User text is omitted unless asked
testdata/
docs/adr/
docs/guide.md
examples/two-apis/
```

No `util`, `common`, `pkg`, or empty directories. A new package needs a caller in this repo.

## Signed decisions

1. `veto serve` executes from the catalog with net/http. `generate` writes an optional Go client that calls `runtime.Invoke`. Serving does not require codegen.
2. MCP registers search, describe, and invoke, plus `--pin`. Never one tool per operation.
3. DELETE, or an id containing "delete", requires confirmation unless agent.yaml sets confirmation false for that operation. agent.yaml can require confirmation on any operation. Without approval, invoke does not call HTTP.
4. Context pack holds rules, the search hits and their neighbors, the conversation given to it, the operation just described, and pending confirmation. It does not list every operation and it does not embed the raw spec. Truncate the index first.
5. Semantics come from the summary, tags, path noun, and a small synonym map (delete/remove/retire, get/fetch/read). A declared relation becomes a sentence on the note, such as `Order.customerId identifies customers.get`. A yaml overlay overrides one id and does not drop that sentence. Apache Ossie 0.1.1 is the later file format behind `semantics.Provider`. Do not import `github.com/apache/ossie/cli`. Do not depend on Ossie 0.2 drafts.
6. No external decision client is in the module. A decision check does not approve a delete.
7. Memory, model, semantics, and policy have one default each. `scripted` is the model default. `model: openai` is optional and reads `OPENAI_API_KEY`. Subagents and a vector store are not packages.
8. Evals use the scripted model and the real policy path. The shipped case is "delete order 123": confirmation required, delete operation named.
9. Protobuf is not implemented.
10. `veto.yaml` lists the contracts, relations, semantics, agent metadata, and provider keys. Repeat `--contract` only to override that list. A relation file joins a schema field to an operation. A field name alone does not. Each contract keeps its server URL. Ossie is not imported.

## Docs, comments, tests

One fact lives in one place. No fluff docs. No second doc that repeats the first. No fluff comments. A comment only where the next reader would otherwise guess wrong. No test that does not lock a behavior.

README: what veto is, the module path, and the three commands. No tour.

```
veto validate --contract testdata/orders.yaml
veto serve --contract testdata/orders.yaml --stdio
veto eval --contract testdata/orders.yaml --case testdata/delete.yaml
```

Tests use the standard `testing` package and testify. They lock behavior: graph grouping, search through synonyms, context pack omits the raw spec, delete does not hit the test server until approved, eval passes, MCP tool list is the three capabilities plus pins.

## Shape

Modular. The core decides. The edge adapts.

- Core packages hold the logic: catalog, semantics, runctx, policy, flow, memory, and the `agent` loop. They return values and errors. They do not log, print, exit, or know about MCP or the CLI. `runtime.Invoke` is the policy gate for the CLI, MCP, and generated clients. `agent.Invoke` is one loop turn and calls that runtime. `execute` is the only HTTP writer.
- Edges are `cmd/veto`, `mcpserver`, and `execute`. They parse input, call the core, and handle the error once: an exit code, an MCP error, or an HTTP status.
- Wrap an error on the way out. Do not log it and return it.
- Validate at the edge. Pass typed values inward. Do not pass a raw request, a flag set, or the environment into the core.
- Imports point toward the core. A core package does not import an edge.

## Go

Idiomatic Go. Dave Cheney and Mat Ryer.

- Accept interfaces, return structs. A constructor returns the concrete type. Callers depend on a small interface.
- The bigger the interface, the weaker the abstraction. One method is the usual size. Define that interface next to the code that uses it, so a test can pass a fake.
- Start with the struct. Add the interface when a second implementation, or a test, needs it.
- Make the zero value useful.
- Errors are values. The core wraps them with context (`fmt.Errorf("load spec: %w", err)`) and returns them. The edge handles them. Assert behaviour, not a concrete error type.
- A little copying over a dependency you do not need. Clear over clever.
- Line of sight: happy path on the left, return on the error, no else that only returns, the success return is the last line. Keep the function small enough to see at once.
- Dependencies are fields, set by the constructor. No package-level mutable state.
- `context.Context` is the first parameter when the call can be cancelled.
- Table-driven tests when the cases share one behavior.
- Never start a goroutine without knowing how it stops.
- Exported names are one sentence.
- `gofmt` every file you touch.
- Package block order is const, then var, then type, then func. When a file has more than one of a kind, group them: `const ( )`, `var ( )`, `type ( )`. The doc comment stays on the declaration it describes. Methods sit with the funcs, grouped by receiver: constructor, then methods, then helpers. A const or var inside a function stays in that function.

## Where the code is

Work in the checkout of `github.com/aiveto/veto`. `gofmt` the files you touch.

## Round table

Before an implementation is locked, review the change as a round table. The staff engineer and the solutions architect each write what is wrong, what they would do, and the decision. Then code.

A signed decision is locked only with a short file in `docs/adr/` that holds those notes. Do not reopen a decision inside a drive-by edit.

## Not yet

Ossie 0.1.1 reader. Protobuf loader. Subagents.
