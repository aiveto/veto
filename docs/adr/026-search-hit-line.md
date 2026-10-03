# ADR 026: Search returns a call line

## Context

`capabilities_search` marshaled the full `catalog.Operation` on every hit. Eight hits were eight describes. The model still has `capabilities_describe` for the operation row. People asked whether an HTTP pool, a singleton, or MCP `structuredContent` would make the loop faster.

## Staff engineer

Search is discovery. Describe is the contract. A hit should be the pack line, related ids, and whether confirmation is on. Describe stays whole. Do not change the invoke body default. `response_fields` is opt-in so a list endpoint does not silently drop fields.

## Staff Go engineer

`http.Client` with a nil Transport is `http.DefaultTransport`. That is the process idle pool. `boundRedirects` copies the client and keeps the Transport. Auth and execute already share that pool. Do not add a pool package, a singleton catalog, or `sync.Pool` for eight hits. `catalog.Match` stays the in-process rank result. The wire type lives on the MCP server.

## Staff AI engineer

Tokens on the tool result are the cost, not JSON vs prose. `structuredContent` is a second copy of the same JSON. An `outputSchema` makes `tools/list` heavier. Keep three tools. Pins already skip hops. `confirmation` on the hit stops a delete surprise without another describe.

## AI architect

ADR 003 is unchanged. Do not collapse search and describe. Do not add a vector index or a connection multiplexer. Hosts that want typed results can come later.

## Decision

`capabilities_search` returns `id`, `call` (the pack line), `related`, and `confirmation` when the gate is on. Describe still returns the operation, semantics, schemas, and the relation sentence. No new HTTP client. No `structuredContent` for speed.

## What we refused

A custom transport pool. `sync.Pool`. JSON for the pack. An output schema on search. Changing the invoke body default. Skipping describe as a new default.
