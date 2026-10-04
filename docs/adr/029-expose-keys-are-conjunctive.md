# ADR 029: Exposure keys are conjunctive

`expose.tags` and `expose.paths` are OR within each list. An operation must pass every key that is set. `read_only` is another key. ADR 022 kept an operation that matched a listed tag or a listed path.

Refused: a tag match that keeps an unmatched path. A deny list.
