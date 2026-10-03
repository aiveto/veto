# ADR 020: A deployment may turn confirmation off

`confirmation` on `veto.yaml` is a bool. Unset and `true` leave the gate on. `false` clears `RequiresConfirmation` on every operation after `agent.yaml`, so one operation set to true does not turn the gate back on. `agent.yaml` can still exempt one operation when the deployment key is unset.

Doctor and check print `confirmation is off`. That line does not fail doctor. `--against` does not also report each lost confirmation. A bundle manifest cannot carry the key. Invoke has no argument for it. Serve, doctor, check, validate, generate, and the git baseline apply the same clear. A Rego confirmation decision still stops the call.

Refused: an environment variable. An MCP argument. A global off that one `agent.yaml` entry can undo.
