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

- **Employer** (also client or private work) per person, each with its own time zone and public holiday region, and a
  **holder**: `private` (the person's own records: only the person sees them and may change, export and erase them
  freely) or `managed` (the records belong to an organization, see below).
- **Organizations** are a concept of their own, in the standalone program as in an embedding application (UnderDoc
  maps a tenant to one organization). An organization has administrators and members (memberships with a role and a
  period), schedule templates for its members, its retention per deletion class (at least two years) and the company
  features (approval, a supervisor's present/absent view), all off by default and only switchable with the reference to
  a works agreement and who confirmed it when. A person can be a member of several organizations and keep private
  employers at the same time; each membership has one managed employer that holds the person's records there.
- **Strict separation:** an organization only ever reads its managed employers and the records bound to them
  (`Store.Managed`), never anything private. Records for every employer (an absence, an own holiday, an own absence
  type) only count for private employers, so nothing private reaches an organization without the person's own step,
  and the person and the organization see the same numbers for a managed employer. A test proves it for every store.
- **Rights** follow the holder: the settings of a private employer belong to the person, those of a managed employer
  to the organization's administrators; only the person books, and managed records leave only through the
  organization's retention purge, never through the person's erasure.
- **The person's overview** sums all employers, managed and private. It warns the person when the work for all
  employers together passes the limits of the German Working Hours Act (more than 10 hours a day, more than 48 hours a
  week, less than 11 hours of rest; hours of several employers count together, § 2 Abs. 1 ArbZG). Clients and private
  work do not count. The hints are for the person only and never reach an organization.
- **Leaving an organization** ends the membership and archives its employer: the managed records stay with the
  organization, the person takes a copy (the full export). Private records stay with the person, who can export them,
  take them over into another instance, or erase them.
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
- **Company features** (approval, a supervisor's view) belong to the organization and are off by default.

## Consequences

- Sums come from `minutes`; start and end are extra detail. If both are given, start and end win.
- In the mode `plan` the days are not stored: the calculation derives them from the schedule until the person switches
  to `exact`, which turns them into entries with the same minutes per day.
- Earlier balances and earnings stay stable because schedules and rates are versioned, not edited.
- The holder, the organizations and the separation were added on 2026-11-17 at the maintainer's request, before any
  release; the standalone program supports both holders.
