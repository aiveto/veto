# ADR 006: OpenAPI via kin-openapi; no protobuf package

## Context

ADR-001 mentions gRPC later. This repository slice is OpenAPI-only.

## Staff engineer

`kin-openapi` is the common choice in Go for loading OpenAPI 3 documents. Ship one loader that works in tests and CLI.

## Architect

An empty `protobuf/` package reserves the layout but adds noise until a loader exists.

## Decision

Load contracts with `github.com/getkin/kin-openapi`. Do not add a protobuf adapter or an empty proto package in this slice.

## What we refused

Placeholder packages and partial gRPC support that imply parity without implementation.
