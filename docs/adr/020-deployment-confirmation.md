# ADR 020: A deployment may turn confirmation off

## Context

Confirmation is inferred from DELETE or an id containing "delete". `agent.yaml` can set `confirmation: false` on one operation. A deployment that does not want the gate has to repeat that line on every delete, and a new delete waits until someone remembers it.

## Staff engineer

A plain `bool` is wrong: the zero value is false, so an omitted key would turn the gate off. The key belongs on `veto.yaml`, after `agent.yaml` is applied, and it clears `RequiresConfirmation` on every operation. A bundle that can set the key would ship the off switch. `--against` must not report every delete as a lost confirmation when the key is the record. Doctor must say the gate is off and must not fail the command for that line.

## Architect

`veto validate` and `veto generate` do not go through `buildLoop`. A client generated without the key would still confirm. The git baseline for `--against` has to read the key too. A Rego decision of confirmation is an explicit policy, and it stays in force.

## Decision

`confirmation` is a bool on `veto.yaml`. Unset and `true` leave the gate on. `false` clears `RequiresConfirmation` on every operation after `agent.yaml`, so one operation set to true does not turn the gate back on. `agent.yaml` `confirmation: false` still exempts one operation when the deployment key is unset.

Doctor and check print `confirmation is off`. That line does not fail doctor. `--against` does not also report each lost confirmation. A bundle manifest cannot carry the key. Invoke has no argument for it. Serve, doctor, check, validate, generate, and the git baseline all apply the same clear. A Rego confirmation decision still stops the call.

## What we refused

An environment variable. An MCP argument. A global off that one `agent.yaml` entry can undo. Failing doctor because the gate is off.
