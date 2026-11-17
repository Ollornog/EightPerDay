---
id: T-2
type: Task
title: "Access log for the person concerned (A8)"
status: erledigt
milestone: M-3
tags: [core, privacy, audit]
created: 2026-11-17
---

`AccessLog` with both stores, migration 002, `ReadManaged`, `VerifyAccesses`, the access log in the export and
`storetest.RunAccessLog`. Embedding applications run migration 002 in their own migration flow and log what their
organizations read, change and export.

Not covered: an anchor of the last hash outside the database, against someone who rewrites a whole chain.
