# ADR 022: Expose part of a contract, and check the body

Superseded in part by [ADR 029](029-expose-keys-are-conjunctive.md): tags and paths are OR within each list and AND across configured keys. This decision kept an operation that matched a listed tag or a listed path.

`veto.yaml` takes `read_only: true` and `expose: {tags, paths}`. Unset keeps every operation. The rest are removed from the catalog after `agent.yaml`, on both sides of `--against`. Path prefixes stop on a segment boundary. A bundle cannot carry these keys.

A JSON request body is checked against the operation's schema before policy, credentials, and HTTP. A failure is `invalid_body` and names the path and the rule. A query, path, or header value is checked against its schema. A failure is `invalid_param`. Neither echoes the value.

Refused: hiding by `agent.yaml` per operation. A deny list. Glob patterns. A second schema library.
