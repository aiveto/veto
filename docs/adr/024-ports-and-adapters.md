# ADR 024: Core stays business, edges hold process and I/O

Superseded in part by [ADR 025](025-export-store.md): `policy.Store` is public. Memory and Files ship. `valkey` is the replica adapter.

`policy.State` talks to a store and a signer. Memory is the default. Files and HMAC sit behind `SetNonceDir` and `SetSigner`. The invoke gate is constructed by the owner of the Runtime. A bundle is a value; the CLI holds extracted zips and releases them on exit.

Refused: a process list inside bundle. A package mutex so Runtime can be copied before anyone sets Gate.
