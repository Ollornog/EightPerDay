---
id: T-11
type: Task
title: "Hooks for an embedding application: mount path, its sign-in, its tenant, a frame, a calendar"
status: erledigt
milestone: M-3
tags: [embedding, ui, security]
created: 2026-11-19
---

What UnderDoc needs to run the interface of the standalone program inside its workspace, without a copy of it:

- `web.Options.Embedding`: the mount path (`Base`), the account of a request from the application's sign-in
  (`Account`), its accounts (`Directory`) and where it keeps its theme (`ThemeKey`). Embedded there is no login,
  setup, account or admin page and no organization is founded; every link and redirect stays below the mount path,
  changes are accepted from the request's own host, and only pages of the same origin may frame the interface.
- Stopwatches in the store (`core.Stopwatches`, table `<prefix>timers`), so they live in the application's tenant.
- The access log keeps its rules as one permissive policy per tenant plus restrictive ones (migration 003), the shape
  the tenant guard of an application reads; deleting a whole tenant for good reaches it through
  `Options.EraseSetting`.
- `core.CalendarAbsences` gives the absences of a range for a calendar.
- `TestNoTenantReachesAnother` proves for every method of the store that no tenant reaches another.
