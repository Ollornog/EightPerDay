---
id: M-3
type: Milestone
title: "Embedding: another application runs the core in its own database"
status: offen
blocked_by: [M-1]
tags: [core, embedding]
created: 2026-11-17
---

An application such as UnderDoc embeds the library, runs its migrations in its own flow and builds its own interface.

**Done when this is true:**

- The migrations can be fetched as plain SQL with the application's table prefix and tenant setting, and run by the
  application in its own migration flow.
- A test proves that one tenant cannot read or change another tenant's rows through any store method.
- The company features (approval, a supervisor's view) exist as hooks in the data model and are off by default.
