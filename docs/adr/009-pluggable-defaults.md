# ADR 009: Small interfaces with one default each

## Context

Memory, model, semantics, and policy will swap in enterprise deployments.

## Staff engineer

Define narrow interfaces and ship one in-memory or scripted default per concern. Configuration is yaml overlays, not a sprawl of packages.

## Architect

Early interfaces for OSSIE tempt premature abstraction.

## Decision

Interfaces: `agent.Memory`, `agent.Completer`, `semantics.Provider`, `policy.Hook`. The local map is `memory.LocalMap`. Defaults: that map, the scripted model, derived+file semantics, builtin policy. Document optional provider keys in ADR 012. They are not packages in this slice.

## What we refused

Bundling OSSIE clients or a subagent package in the core tree.
