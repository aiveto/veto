# ADR 016: One catalog, many APIs, explicit relations

`veto.yaml` lists the contracts, the relations file, semantics, agent metadata, and provider keys. Repeat `--contract` only to override that list. Duplicate operation ids fail the load. A relation file joins a schema field to an operation. A field name alone does not. Each operation keeps the server URL from its own spec. `--base-url` overrides all of them. Ossie is not a client in this module.

Refused: a foreign-key heuristic. One tool per operation.
