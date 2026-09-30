# ADR 013: agent.yaml overlays the catalog

## Context

Confirmation is inferred from DELETE or an id containing "delete". A publish or a retire that is not a DELETE cannot ask for confirmation, and permissions have nowhere to live.

## Staff engineer

Keep the inference as the default. A separate file sets side effect, confirmation, permissions, idempotency, retry, and exposure on an operation id. A missing file leaves the default. A field present in the file replaces the derived value, including turning confirmation off.

## Architect

OpenAPI vendor extensions would keep one file. They also invent an annotation dialect the contract does not own.

## Decision

`agent.yaml` is merged by `agentmeta.Apply`. Unknown operation ids are an error. Exposure is `direct`, `grouped`, or `discovery-only`. Config key `agent_file`. A command `--agent` flag overrides that key.

`RequiresConfirmation` is the only policy gate. `confirmation: false` allows the call even when the side effect stays `destructive`. Setting `side_effect: destructive` and omitting confirmation turns the gate on.

## What we refused

A new OpenAPI annotation dialect. A permission language. Retry execution.
