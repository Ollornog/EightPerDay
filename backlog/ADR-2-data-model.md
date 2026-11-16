---
id: ADR-2
type: Decision
title: "One entry table, modes as views, everything with a valid-from date"
status: erledigt
tags: [architecture, data-model]
created: 2026-11-17
---

## Context

People track time very differently: a weekly total, a fixed schedule where only deviations count, exact spans split
across projects, or only time per project. Many have more than one employer or client, each with its own schedule and
pay. Existing tools force one way, lack target and leave, or rewrite the past when a schedule or rate changes.

## Options

- **A schema per mode:** switching modes means converting data, and something gets lost.
- **One model, where the mode only decides which fields are required and which view is shown.**

## Decision

The second option.

- **Employer** (also client or private) per person, each with its own time zone and public holiday region.
- **Mode changes** with a valid-from date per employer: `week_sum`, `schedule`, `plan` (the schedule counts as worked,
  only deviations are booked), `exact` and `projects_only`. The calculation picks the mode valid on each day; switching
  never rewrites an entry.
- **Schedules** with a valid-from date: minutes per weekday, optional start, end and break, the overtime basis (day,
  week or month) and leave days per year.
- **Pay rules** with a valid-from date and an optional end, for the employer or one project: per hour, per project, per
  assignment (day or job) or per period. The most specific rule valid on the date wins.
- **Entries** in one table: `minutes` always, start and end optional (UTC plus the local date and time zone), a
  precision (span, day, week), the kind (`work`, `break`, `adjust` for a signed deviation), project, activity, tags,
  and the rate applied as a snapshot that is only re-valued on purpose.
- **Absences** with a type whose effect is data: it fills the target (the day counts as worked) or reduces it, or is
  for information only. Sickness is a flag without details.
- **Public holidays** as data per region (the German states and Austria), extendable, and own holidays per employer.
- **Corrections** never overwrite: every change and deletion keeps the previous state with who, when and why. Closed
  periods are locked.
- **Company features** (approval, a supervisor's view) are fields in the model and off by default.

## Consequences

- Sums come from `minutes`; start and end are extra detail. If both are given, start and end win.
- In the mode `plan` the days are not stored: the calculation derives them from the schedule until the person switches
  to `exact`, which turns them into entries with the same minutes per day.
- Earlier balances and earnings stay stable because schedules and rates are versioned, not edited.
