# ADR 022: Expose part of a contract, and check the body

## Context

A security team's first question is how to expose only part of an API. Today every operation in the contract is searchable, and agent.yaml can only make one operation discovery-only. A request body is sent if it is a JSON object, whatever the schema says, so a wrong field reaches the upstream API.

## Staff engineer

Selection belongs on `veto.yaml`, beside `confirmation`, after `agent.yaml`. A hidden operation must be gone from search, describe, invoke, pins, and generate, and from the relation sentences on other notes. The graph already drops an edge to an operation that is not in the catalog, so removing the operation is enough. The zero value keeps everything. Path entries are prefixes on a segment boundary, so `/orders` does not match `/ordersarchive`.

The body check runs where the parameter check runs, in the catalog, so the CLI, MCP, the generated client, and eval share it. The stored schema is the inlined subset the loader keeps, so the check can be looser than the contract and never stricter. The error names the field path and the rule. It does not echo the value.

## Architect

`read_only: true` keeps GET and HEAD. Discovery-only would leave writes in search, and a read-only deployment means the agent does not see them. A bundle must not carry these keys, for the same reason it cannot carry `confirmation`. `--against` compares what each side serves. The git baseline already applies its own `confirmation` key, so it applies its own selection too. A cut that drops a joined operation fails, and a wider cut that lets in a destructive operation fails, which is the change a reviewer needs to see.

## Decision

`veto.yaml` takes `read_only: true` and `expose: {tags: [...], paths: [...]}`. An operation stays when it matches a listed tag or path prefix, and, under `read_only`, is GET or HEAD. Unset keeps every operation. The rest are removed from the catalog after `agent.yaml`, on both sides of `--against`. A JSON request body is checked against the operation's schema before policy, credentials, and HTTP. A failure is `invalid_body` and names the path and the rule.

## What we refused

Hiding by agent.yaml per operation. A deny list. Glob patterns. A second schema library. Echoing the rejected value.
