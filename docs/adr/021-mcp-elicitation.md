# ADR 021: A person approves a held call in the chat

Stdio asks the person when the client advertises elicitation. The form is a confirm schema. `--http` asks only when `veto.yaml` sets `chat_approval: true`. Unset over HTTP ignores an elicitation answer, and `veto approve` stays the approval. `veto invoke` asks the same sentence when stdin and stdout are a terminal; `y` records the approval and runs that call once. Accept records the approval and runs that call once. Decline or cancel leaves the pending id. A bundle manifest cannot carry the key.

Refused: a `capabilities_approve` tool. An HTTP route the same caller can use to approve their own delete. Treating the model's next tool call as the approval. Chat approval on by default over HTTP.
