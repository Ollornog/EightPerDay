---
id: T-10
type: Task
title: "Several employers in one list, with their pay, extras, kilometres and standby"
status: erledigt
milestone: M-5
tags: [ui, core, pay]
created: 2026-11-19
---

- All employers in one list, a column each only on the days they are active.
- Per employer its pay: per hour, per day or half day, per job with a client and an amount, plus extras such as
  commission, photos and tips, taken in cash or by card; the earnings per day, week and the average day.
- Optionally per employer a place of work with kilometres (one way, in all) per day, and standby (passive, active).
- An optional estimate of pay from a yearly or monthly salary or an hourly rate, with 13th and 14th month pay.

Done: `/all` with the columns, earnings, average and cash/card; `core.Income` and `core.DayInfo` on the day page;
`PayRule.Salaries` for 13th and 14th month pay. Pay per half day is booked as two jobs of a day (per_assignment with
unit assignment) or as an income per lesson; standby is recorded, not paid.
