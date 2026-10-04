# ADR 028: CLI search, describe, and invoke

`veto search`, `veto describe`, and `veto invoke` call the same `mcpserver.Server` methods as `capabilities_search`, `capabilities_describe`, and `capabilities_invoke`. `--help-json` on those commands prints `mcpserver.Capability`. A skill unmarshals the same JSON the tools take. Catalog flags stay process setup. The generated per-API CLI stays typed invoke.

Refused: a second search path. A second help schema.
