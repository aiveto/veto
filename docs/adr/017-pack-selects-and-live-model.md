# ADR 017: The pack selects, relations become sentences, openai is a key

## Context

The pack listed every operation id. A declared relation was an edge the user could see only by reading the graph. The only model was a regex, so the pack had nothing real to read it.

## Staff engineer

Search the user message. Keep those operations and their neighbors. Drop the rest. Turn `Order.customerId` into the sentence `Order.customerId identifies customers.get` on the semantic note, so a semantics file is optional. `model: openai` sends that pack to an OpenAI-compatible endpoint. The API key stays in `OPENAI_API_KEY`. `scripted` stays the default so eval needs no network.

## Architect

Put the whole catalog in the pack and let the model ignore what it does not need. Ship a vendor SDK.

## Decision

The pack is the selection. The relation sentence is derived. `openai` is a provider key, not a required dependency. An empty key or an empty pack is an error. Unknown model names still fail config load.

## What we refused

A full-catalog index. A guessed relation. An OpenAI SDK dependency.
