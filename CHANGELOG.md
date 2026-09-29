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
