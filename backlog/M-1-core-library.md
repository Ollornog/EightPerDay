---
id: M-1
type: Milestone
title: "The core library: model, calculations, parser, storage"
status: erledigt
tags: [core]
created: 2026-11-17
---

The Go library that everything else stands on (ADR-1), with the data model of ADR-2. No HTTP, no sign-in.

**Done when this is true:**

- Employers and clients per person, each with operating modes, schedules and pay rules that carry a valid-from date.
- Entries (one table, `minutes` always, start and end optional, rate as a snapshot) and absences with types that either
  fill or reduce the target.
- Target, actual and balance per day, week, month and year; overtime account, leave account, public holidays of the
  German states and Austria as data, earnings per employer and period.
- Switching the operating mode loses nothing, in both directions.
- A free-text parser for entries such as `2h #tag @Project note` and `8-16:30 30m break`, with a fuzz test.
- A correction history instead of overwriting, configurable retention with deletion classes, and an export for the
  person concerned (CSV, JSON).
- A storage interface with a PostgreSQL implementation (embedded migrations, table prefix, tenant column, row-level
  security on a configurable setting) and an in-memory one for tests.
- Organizations with members and administrators; employers held privately or managed by an organization, strictly
  separated; the person's overview over all employers with hints on the limits of the Working Hours Act.
- Table tests for every rule: month ends, daylight saving time in Europe/Berlin, a schedule that changes in the middle
  of a month, part time, public holidays, mode switches.
