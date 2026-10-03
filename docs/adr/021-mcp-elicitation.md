# ADR 021: A person approves a held call in the chat

## Context

A destructive invoke returns a pending id and does not call HTTP. `veto approve` records the yes on the machine that holds the approval files. A person using Claude Desktop, Cursor, or another MCP host has no terminal on that machine.

## Staff engineer

The model must not gain a fourth tool that approves its own delete. The Go MCP SDK already has elicitation: the handler returns an input request and a request state, and no tool content. Content together with an input request is a server bug. Accept calls `State.Approve` on that state, then the same invoke. Decline returns `confirmation_required` and the pending id. A client that did not advertise elicitation keeps today's JSON. Check the pending record's operation before `Approve`, so a swapped state does not approve a different call.

## Architect

Elicitation trusts the client to show the form to a person. Claude Desktop and Cursor do. A program built on the SDK can answer accept itself. Over stdio the client is the person's own host, so that adds nothing. Over `--http` the caller is someone else, and a self-answered accept is the same caller approving their own delete.

## Decision

Stdio asks the person when the client advertises elicitation. `--http` asks only when `veto.yaml` sets `chat_approval: true`. Unset over HTTP ignores an elicitation answer, and `veto approve` stays the approval. Accept records the approval and runs that call once. Decline or cancel leaves the pending id. `veto approve` still prints the id a later invoke accepts once. A bundle manifest cannot carry the key.

## What we refused

A `capabilities_approve` tool. An HTTP route the same caller can use to approve their own delete. Treating the model's next tool call as the approval. Chat approval on by default over HTTP.
