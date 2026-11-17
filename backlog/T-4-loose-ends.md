---
id: T-4
type: Task
title: "Loose ends: first link, region names, reviews, reads once a day, reminders, legal templates"
status: erledigt
milestone: M-3
tags: [security, ui, company-features, privacy]
created: 2026-11-17
---

- The first administrator's link never goes to the log: it comes from `eightperday link --admin` at the console,
  lasts one hour and replaces the one before; the image check fails on a link in the log.
- Regions of public holidays carry readable names (English, German), the parts of a state included.
- An administrator's own request is marked on the organization's page; another administrator decides it.
- The reviews read the days of the open requests (`core.ReviewRange`) instead of a fixed span.
- The organization's page logs its reads once per member, view and day (`core.ReadManagedDaily`,
  `core.LogAccessOnce`), without touching the chain.
- Leave reminders (`core.LeaveReminders`) on the person's week view; the page `/legal` shows and hands out the
  templates of `docs/legal` and the data inventory, marked as no legal advice and not checked by a lawyer.

Not covered: recording the employer's notice on lapsing leave in the interface (`LeaveNotice` comes from the core
library so far), and the organization's list of the notices it still owes.
