# ADR 013: agent.yaml overlays the catalog

`agentmeta.Apply` merges `agent.yaml` onto the catalog. A missing file leaves the derived defaults. A present field replaces the derived value. Unknown operation ids fail the load. Exposure is `direct`, `grouped`, or `discovery-only`. `--agent` overrides `agent_file`.

`RequiresConfirmation` is the only policy gate. `confirmation: false` allows the call even when the side effect stays `destructive`. Setting `side_effect: destructive` and omitting confirmation turns the gate on.

Refused: an OpenAPI annotation dialect. A permission language. Retry execution.
