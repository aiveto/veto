# ADR 013: agent.yaml overlays the catalog

Superseded in part: confirmation is not the only policy gate. Builtin permissions, OPA, and the deployment confirmation key also decide.

`agentmeta.Apply` merges `agent.yaml` onto the catalog. A missing file leaves the derived defaults. A present field replaces the derived value. Unknown operation ids fail the load. Exposure is `direct`, `grouped`, or `discovery-only`. `--agent` overrides `agent_file`.

`RequiresConfirmation` is the catalog confirmation flag. `confirmation: false` allows the call even when the side effect stays `destructive`. Setting `side_effect: destructive` and omitting confirmation turns that flag on. An OPA allow still goes through builtin permissions and confirmation.

Refused: an OpenAPI annotation dialect. A permission language. Retry execution.
