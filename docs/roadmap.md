# Roadmap

Pre-1.0. These are the next things that change how someone installs veto, finds a held delete, or loads a contract we cannot read today.

## Next

**Homebrew.** `brew install veto` from the release binaries. `go install` stays. The person without Go can run `serve` and `approve`.

**`veto pending`.** List held calls from the store the server already uses. `veto approve` still records the yes. The person does not have to copy the id out of the agent.

## When a deployment needs it

**mTLS.** A client certificate when the contract asks for one. Bearer, API key, OAuth2, a command, and a Go signer already ship.

**SQLite store.** Consume-once on one machine without Valkey. Files stays for one process. `SetStore` stays the wiring.

**Protobuf.** A `.proto` becomes a catalog the same way an OpenAPI file does. No empty package until that loader exists.
