---
id: ADR-3
type: Decision
title: "Half days on the work, standby as pay rules, one currency"
status: erledigt
tags: [data-model, pay]
created: 2026-11-19
---

## Context

Pay per day often knows a morning and an afternoon (a ski school pays a half day of lessons less than a whole one),
and some employers pay standby: passive (reachable only) and active (called out), each per hour or flat per standby
day or call-out. `DayInfo` already kept the minutes of both kinds of standby, unpaid. Several currencies were asked
for as well.

## Options

- **Half days:** (a) two jobs of a day with unit assignment (the old workaround, no own amount); (b) a field on the
  absence-like day details; (c) a half of the day on the work entry, and an amount for half a day on the pay rule.
- **Standby:** (a) a new record kind with its own rates and valid-from dates; (b) fields on the employer; (c) two new
  bases of `PayRule`, with the units it already has.
- **Currencies:** amounts per currency in every sum and view, or one currency per instance and employer.

## Decision

- **Half days: (c).** `Entry.Part` (`am`, `pm`) says which half the work took; `Day.Part` derives the day's half from
  its work, so a morning and an afternoon of the same day are a whole day without anyone saying so. `PayRule.HalfAmount`
  holds the amount for half a day of a rule per day, half of `Amount` without one. The part lives where the hours are
  typed, the amount where the pay is, and both keep the valid-from history of their records.
- **Standby: (c).** `standby_passive` and `standby_active` are pay bases like `hourly`: no unit pays per hour, `day`
  per day with standby of the kind, `assignment` per call-out (active only, counted in `DayInfo.Callouts`). This is
  the smallest model that carries both kinds and both ways of paying: the valid-from dates, the end, the currency and
  the most specific rule are those of every pay rule, the settings page and the earnings lines need no new record kind,
  and the units already exist. A new record kind would repeat all of that; fields on the employer would lose the
  history of rates.
- **Standby as working time** is a setting of the employer (`Employer.StandbyWork`: `active` by default, `none`,
  `all`), not of a rate, because it matters without any pay. `Day.Worked` holds the part that counts (`Day.Standby`),
  so the balance, overtime and the hints of the Working Hours Act see it; hourly pay, pay per day and the average of
  paid leave leave it out, so nothing is paid twice.
- **One currency per instance and employer**, as before. Several currencies are rejected (maintainer, 2026-11-19):
  every sum would need amounts per currency and an exchange rate nobody keeps.

## Consequences

- Records without `Part`, `HalfAmount`, `Callouts` or `StandbyWork` keep their meaning: whole days, half the amount,
  one call-out per day with active minutes, active standby as working time. No migration: the stores keep the record
  as a document.
- Active standby now counts as working time by default where it was recorded before; an employer that does not count
  it switches it off.
