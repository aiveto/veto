# ADR 012: Future providers are config keys, not core packages

## Context

ADR-001 names decision providers (e.g. Jev), semantic catalogs (e.g. OSSIE), and durable execution (e.g. Temporal) as optional integrations.

## Staff engineer

Extension points belong in configuration and interfaces, not in the default module graph.

## Architect

First-class packages for each vendor speed up demos for those ecosystems.

## Decision

Document provider keys in config for model, memory, semantics, decision, policy, telemetry, execution, and subagents. Defaults stay in-tree; Jev, OSSIE, Temporal, and subagents are not imported in this slice.

## What we refused

Shipping vendor SDKs or workflow engines as required dependencies.
