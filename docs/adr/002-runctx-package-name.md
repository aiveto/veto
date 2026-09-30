# ADR 002: Context pack package is `runctx`

## Context

The framework builds a bounded context pack for each run (rules, index, turns, described operation, pending confirmation).

## Staff engineer

A package named `context` collides with the standard library and confuses imports in every file that needs both.

## Architect

Names like `agentctx` or `pack` could work; consistency with other short package names matters less than avoiding stdlib shadowing.

## Decision

The context pack lives in package `runctx`. Never use package name `context` for this concern.

## What we refused

A top-level `context/` package in this module.
