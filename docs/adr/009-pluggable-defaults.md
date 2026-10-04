# ADR 009: Small interfaces with one default each

Superseded in part: `semantics.Provider` is `semantics.Notes`.

The interfaces are `agent.Memory`, `agent.Completer`, `semantics.Notes`, and `policy.Hook`. Defaults are `memory.New`, the scripted model, derived plus file semantics, and builtin policy. Optional provider keys are in ADR 012.

Refused: Ossie clients or a subagent package in this tree.
