# ADR 016: One catalog, many APIs, explicit relations

## Context

One `--contract` made veto a single-spec MCP server with a confirmation check. That is not a framework. The catalog has to be where every API lands, and the context pack has to walk the edges.

## Staff engineer

One `veto.yaml` lists the contracts, the relations file, semantics, agent metadata, and the provider keys. `veto serve --config` is the program. Repeat `--contract` only to override the list. Merge into one catalog. Duplicate operation ids fail the load. A relations file asserts `Order.customerId` identifies `customers.get`. Every operation that uses `Order` gains that edge. Search, describe, and the context pack show it. Each operation keeps the server URL from its own spec. A `--base-url` flag overrides all of them.

## Architect

Guess the edge from the field name so customers appear as soon as the second API is registered.

## Decision

Do not guess. `customerId` creates no edge. The relation file does. Model, memory, semantics, policy, and in-process execution stay the hook points. Temporal, Jev, and Ossie are config keys for a later provider. They are not clients in this module.

## What we refused

A foreign-key heuristic. A Temporal package. One tool per operation.
