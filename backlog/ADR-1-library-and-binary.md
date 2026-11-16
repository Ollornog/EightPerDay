---
id: ADR-1
type: Decision
title: "A Go library at the core, and a standalone binary on top"
status: erledigt
tags: [architecture]
created: 2026-11-17
---

## Context

EightPerDay is meant to run on its own, and to be embedded into UnderDoc, which builds its own interface and keeps the
data in its own PostgreSQL database with tenants and row-level security.

## Options

- **A service with an API** that UnderDoc calls: two processes, two databases, a second sign-in, and tenants twice.
- **A library only:** no way to use EightPerDay without writing a program.
- **A library at the core and a thin program on top.**

## Decision

The third option. The language is Go, the newest stable release.

- The package `core` holds the data model, the calculations, the parser and a storage interface. It has no HTTP and no
  sign-in. Calculations are pure functions over loaded data, so they are tested without a database.
- `core/pgstore` implements the storage on PostgreSQL. Its migrations are embedded and rendered with a table prefix and
  the name of the setting that carries the tenant, so an embedding application can run them in its own flow, in its own
  database, under its own row-level security. `core/memstore` implements it in memory for tests.
- `cmd/eightperday` with `web/` is the standalone program, a consumer of the library like any other.

## Consequences

- The library's API is a promise to embedding applications; breaking changes need a minor version below 1.0 and a note
  in the changelog.
- The library pulls in nothing but the PostgreSQL driver, and only in `core/pgstore`.
