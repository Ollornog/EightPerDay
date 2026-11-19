---
id: M-3
type: Milestone
title: "Embedding: another application runs the core in its own database"
status: erledigt
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
- UnderDoc maps its tenant to an organization and its accounts to persons, and shows the person's overview only to
  the person.

**Done on 2026-11-19:** the hooks of T-11 (EightPerDay #19) and their use in UnderDoc (UnderDoc #81, b39229f): its migrations run
there with the prefix `time_` and `underdoc.tenant`, `TestNoTenantReachesAnother` covers every store method, the company
features stay off by default, and UnderDoc maps its tenant to an organization, its accounts to persons and its
administrators to the organization's; the personal overview shows only to the person.
