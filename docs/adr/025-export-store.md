# ADR 025: Export Store. Do not ship Redis.

## Context

ADR 024 kept the store port unexported. Two implementations exist: Memory and Files. A replica set cannot share a directory. The question is whether veto ships Redis, SQLite, or the port.

## Staff engineer

Export `Store` and `Record`. `SetStore` is the wiring. Memory and Files stay in this module. Redis is SETNX plus a hash. That is twenty lines in the caller's main, and it needs a server this binary does not run. Putting go-redis in veto makes every `go get` pay for a store most deploys never start.

SQLite would be the in-tree third store if Files fails: one file, no daemon. Not now. No caller in this repo opens a database.

## Architect

90% of veto is one process, or serve plus approve on one machine. Files is that option. HMAC already lets another machine approve without the store. The hole is consume-once across replicas. That hole is the port, not a Redis package.

JWT and mTLS are upstream credentials, not this store. `Token.Extra` does not remove the text/plain transport workaround, so it is not a cleanup.

## Decision

`policy.Store` is public. Memory and Files ship. Redis stays out. A later SQLite store needs a caller and an ADR.

## What we refused

`github.com/redis/go-redis` in this module. A JWT approval token. mTLS until a contract needs a client certificate.
