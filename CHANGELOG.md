# Changelog

## Unreleased

- A JSON body is a catalog parameter. An empty required parameter does not call HTTP.
- Bearer auth uses the contract. The secret is an environment variable named in config.
- The context pack shows the selected call and declared relations. `veto pack` prints it.
- `veto eval` and `veto check` run a case file or a directory. `veto doctor` reports pins and missing auth env names.
- `veto replay --from` prints a redacted trace file. `trace_export: otlp` sends that same attribute set.
- `memory: file` is an optional turn log. Unset memory stays in the process.
- `policy: opa` and `policy: spicedb` are config keys and fail closed.
- `VETO_APPROVAL_SECRET` makes an approval id a signed token. The process does not store it. Unset, confirmation stays in the process.
- `veto check --against` fails when a joined operation disappears, confirmation is dropped without an agent.yaml change, or an eval expectation changes.
- `execution: temporal` and `decision: jev` are config keys and fail closed. The clients are not imported.
- MCP invoke returns `missing_param` in the same JSON shape as other invoke results.
- An eval case can set `no_http`. The case fails when the call reaches HTTP.
- Generated methods return the invoke result, including a missing parameter.
