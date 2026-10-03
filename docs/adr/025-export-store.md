# ADR 025: Export Store. Valkey for replicas.

## Context

ADR 024 kept the store port unexported. Two implementations exist: Memory and Files. Files is one machine, or one shared volume. An enterprise run is more than one `veto serve`. The question is the port, and which replica store veto ships.

## Staff engineer

Export `Store` and `Record`. `SetStore` is the wiring. Memory and Files stay for one process and one box. The replica store speaks RESP: SET, GET, DEL, SET NX. That is Valkey, Redis, Dragonfly, and the managed caches. The client is `valkey-go` (BSD). `go-redis` is Redis Ltd and is not in this module. HMAC verifies a token. Consume-once is always `Store.Claim`, so a signed yes still lands on the shared store.

## Architect

90% of veto is one process, or serve plus approve on one machine. Files is that option. Replicas need one network store, not a shared disk. Name the yaml key `approval_store` (an env var, like auth). The URL is `redis://` or `valkey://`. Do not ship a second client. SQLite stays later: one file, no daemon, if Files fails and there is still no Valkey.

JWT and mTLS are upstream credentials, not this store. `Token.Extra` does not remove the text/plain transport workaround, so it is not a cleanup.

## Decision

`policy.Store` is public. Memory and Files ship. `valkeystore` ships. The server is Valkey or Redis. A later SQLite store needs a caller and an ADR.

## What we refused

`github.com/redis/go-redis`. A Valkey-only URL scheme that rejects Redis. A JWT approval token. mTLS until a contract needs a client certificate.
