# ADR 019: A wrap calls the default

## Context

`policy`, `memory`, and the model host were keys the constructor did not build. Assigning `Loop.Policy` replaced `policy.Builtin`. `model: openai` always used the public OpenAI host.

## Staff engineer

A custom check has to call `Builtin` unless it stops. `model_base_url` belongs on the same OpenAI client. `memory: local` and `policy: builtin` should construct those types. A step list or a plugin loader is a second framework.

## Architect

`veto serve` still cannot load a Go hook. OPA and another memory stay keys for later. The semantics file already patches derived notes. Lock that with a test.

## Decision

`policy.Wrap` returns a hook whose next is `Builtin`. `Loop.WrapPolicy` installs it. `buildLoop` constructs `local`, `builtin`, `scripted`, and `openai` from the keys. Empty `model_base_url` is `https://api.openai.com/v1`.

## What we refused

A plugin registry. One MCP tool per operation. A full invoke-step chain. New providers.
