# ADR 009: Small interfaces with one default each

## Context

Memory, model, semantics, and policy will swap in enterprise deployments.

## Staff engineer

Define narrow interfaces and ship one in-memory or scripted default per concern. Configuration is yaml overlays, not a sprawl of packages.

## Architect

Early interfaces for Jev, OSSIE, or Temporal tempt premature abstraction.

## Decision

Interfaces: `memory.Memory`, `model.Model`, `semantics.Provider`, `policy.Hook`. Defaults: local map memory, scripted model, derived+file semantics, builtin policy. Document optional provider keys (Jev, OSSIE, Temporal) in ADR 012. They are not packages in this slice.

## What we refused

Bundling Jev, OSSIE clients, Temporal, or a subagent package in the core tree.
