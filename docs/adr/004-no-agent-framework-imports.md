# ADR 004: No Eino, LangChain, Temporal, or vector DB imports

## Context

Go agent frameworks already bundle graphs, chains, and integrations. This project is contract-first execution, not another agent stack.

## Staff engineer

Keep dependencies small and the mental model clear: catalog, policy, invoke. Borrow patterns, not packages.

## Architect

Reusing Eino callbacks and graph primitives could speed up a runtime loop.

## Decision

Do not import Eino, LangChain, Temporal, or a vector database. Take explicit steps, span-like callbacks, and interrupt-as-confirmation from those designs in our own code.

## What we refused

Embedding a third-party agent orchestration engine as a core dependency.
