# ADR 025: Export Store. Valkey for replicas.

`policy.Store` is public. Memory and Files ship. `valkey` is the replica store on a `redis://` or `valkey://` URL. `approval_store` names the URL env. Claim is SET NX. HMAC verifies a token. Consume-once is always `Store.Claim`.

Refused: `github.com/redis/go-redis`. A Valkey-only URL scheme that rejects Redis. A JWT approval token.
