# ADR 021: A person approves a held call in the chat

## Context

A destructive invoke returns a pending id and does not call HTTP. `veto approve` records the yes on the machine that holds the approval files. A person using Claude Desktop, Cursor, or another remote MCP host does not have that CLI.

## Staff engineer

The model must not gain a fourth tool that approves its own delete. The Go MCP SDK already has elicitation: the handler returns an input request and a request state, and no tool content. Content together with an input request is a server bug. Accept calls `State.Approve` on that state, then the same invoke. Decline returns `confirmation_required` and the pending id. A client that did not advertise elicitation keeps today's JSON. Check the pending record's operation before `Approve`, so a swapped state does not approve a different call.

## Architect

The form is answered by the person, on the process that held the delete. `veto approve` stays for a host with no elicitation, and for a person who wants the CLI. Two instances still share approval files when the CLI is the path. The request state is the pending id. It is not a new credential.

## Decision

When the client advertises elicitation, a `confirmation_required` invoke returns a confirmation form and the pending id as request state. Accept records the approval and runs that call once. Decline or cancel leaves the pending id. A client without elicitation gets the pending id in the tool result. `veto approve` still prints the id a later invoke accepts once.

## What we refused

A `capabilities_approve` tool. An HTTP route the same caller can use to approve their own delete. Treating the model's next tool call as the approval.
