# ADR 022: Expose part of a contract, and check the body

`veto.yaml` takes `read_only: true` and `expose: {tags, paths}`. An operation stays when it matches a listed tag or path prefix, and, under `read_only`, is GET or HEAD. Unset keeps every operation. The rest are removed from the catalog after `agent.yaml`, on both sides of `--against`. Path prefixes stop on a segment boundary. A bundle cannot carry these keys.

A JSON request body is checked against the operation's schema before policy, credentials, and HTTP. A failure is `invalid_body` and names the path and the rule. It does not echo the value.

Refused: hiding by `agent.yaml` per operation. A deny list. Glob patterns. A second schema library.
