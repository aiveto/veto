# ADR 024: Core stays business, edges hold process and I/O

## Context

`policy.State` mixed the confirmation use case with file writes and HMAC. `bundle` kept a process-wide list of extracted zips. `runtime` used a package mutex so a copied Runtime could lazy-create a limiter. Core was growing edge jobs: process lifetime, disk, and a global lock.

## Staff engineer

The confirmation lifecycle is the core: pending, approve, consume once. Disk and HMAC are adapters the CLI and serve attach. `SetNonceDir` and `SetSigner` stay the wiring. The store and signer interfaces stay unexported until a third implementation needs them.

A bundle is a value. `Close` belongs on that value. The CLI holds extracted zips and releases them on exit. Generated clients construct `Gate` so copies share one limiter. A nil Gate still creates one on first use; concurrent first use is the owner's job.

## Architect

Do not export a plugin Store. Do not add retryablehttp or JWS. The HMAC token and the file layout stay. Spans stay in the decision path; they are Eino callbacks, not logs.

## Decision

`policy.State` talks to a store and a signer. Memory is the default. Files and HMAC sit behind `SetNonceDir` and `SetSigner`. `bundle.Release` is gone. The invoke gate is constructed by the owner of the Runtime.

## What we refused

A public ApprovalStore. A process list inside bundle. A package mutex so Runtime can be copied before anyone sets Gate.
