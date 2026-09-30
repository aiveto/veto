# ADR 010: Sequential flows, no graph engine

## Context

Multi-step automation should be explicit, not buried in prompts.

## Staff engineer

A flow yaml lists operation ids in order. The model may pick a flow name; steps run as code through the same invoke path.

## Architect

A graph engine enables branching and parallelism; it also expands scope toward workflow products.

## Decision

Package `flow` runs a linear sequence only. No graph runtime in this slice.

## What we refused

Building LangGraph-style orchestration in Go for v1.
