# ADR 018: The shape stands

## Context

The tree was reviewed file by file against the Go rules in CLAUDE.md before another lead takes work from it.

## Staff engineer

The catalog, the relation file, the three MCP tools, the pack, and the confirmation gate are the right shape. A few spots were not staff grade. The agent loop imported `execute`, so the core depended on an edge. Confirmation state was a bare map, so two invokes could race, and the stored params aliased the caller's map. MCP dropped JSON marshal errors. The body read error was dropped. Search advertised a score that was always 1. Operation order followed map iteration, so a generated client could change between runs. The loop comment said the result went back to the model. It does not.

## Architect

Keep one catalog, explicit joins, and one policy gate. Do not add a database, a second MCP tool per operation, or a guessed foreign key to make the review look bigger. The heavy lifting that is still missing is a JSON body, auth, the call shape in the pack, and walking a declared relation.

## Decision

The loop accepts an `Executor`. `execute.Client` is the HTTP writer and the only place that builds a request. Confirmation state is locked and keeps its own params. Marshal errors and body errors are returned. Operations are sorted by id. The score field is gone. A second model call stays item 61, because the scripted eval must remain one decision.

## What we refused

A rewrite of the packages that already match the rules. A persistence layer. Implementing items 1 to 8 inside this review.
