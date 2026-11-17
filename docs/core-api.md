# The core library

How to use `github.com/Ollornog/EightPerDay/core` from an application: the standalone program in `cmd/`, or an
application that embeds it. The reasons are in [ADR-1](../backlog/ADR-1-library-and-binary.md) and
[ADR-2](../backlog/ADR-2-data-model.md). Times are minutes, money is in minor units (cents), dates are `core.Date`
(`2026-11-17` in JSON).

## Packages

| Package | What |
|---|---|
| `core` | Model, validation, calculations, parser, export, rights, the `Store` interface |
| `core/memstore` | `Store` in memory, for tests and trials |
| `core/pgstore` | `Store` on PostgreSQL: migrations, table prefix, tenant column with row-level security |
| `core/storetest` | The conformance test every `Store` passes (`Run`, and `RunAccessLog` for an `AccessLog`); run it for a store of your own |

## Model

- `Organization` (members, administrators, schedule templates, retention, company features) and `Membership`
  (person, role, period, the managed employer).
- `Employer` with `Holder` `private` or `managed` (+ `Organization`), `Mode`, time zone, holiday region.
- Catalog records, filed by `Store.Put` and read as a whole with `Store.Catalog(owner)`: `ModeChange`, `Schedule`,
  `PayRule`, `Client`, `Project`, `Activity`, `Tag`, `AbsenceType`, `Confirmation`, `Adjustment`, `PeriodClose`,
  `Holiday`, `LeaveNotice`, `PurgeLog`, `AbsencePay`, `Surcharge`, `Submission`.
- Time records with history: `Entry` (`work`, `break`, `adjust`; `Minutes` always, `Start`/`End` optional) and
  `Absence`. Every create, update and delete writes a `Revision`; updates need the current `Version`
  (`ErrConflict`), closed periods and periods handed in for approval refuse changes (`ErrLocked`).

## Modes

`week_sum` (one total per week), `schedule` (target, balance), `plan` (the schedule counts as worked, only deviations
are booked as `adjust` entries, weeks are confirmed with a `Confirmation`), `exact` (spans and breaks per project),
`projects_only`. A `ModeChange` switches from a date on; nothing is rewritten. `MaterializePlan` turns plan days into
entries before a switch to `exact`; `SpreadWeek` splits a week entry into days.

## Calculations

All of them are pure functions over `core.Data` (catalog, entries, absences), loaded with `core.Load(ctx, store,
owner, from, to)`. `Load` reads entries from the day before `from` on, for night shifts.

| Function | Result |
|---|---|
| `Days(data, employerID, from, to)` | `[]Day`: target, adjusted target, worked, breaks, credited, actual, balance, holiday, absences, minutes per project |
| `Sum(days)`, `Totals(days, PeriodWeek/Month/Year, weekStart)` | Totals per period |
| `Overtime(data, employerID, from, to)` | Time account: balance with adjustments, overtime by the schedule's basis (day, week, month) |
| `Leave(data, employerID, year)` | Leave account: entitlement (prorated), adjustments, taken, requested, remaining |
| `ComputeEarnings(data, employerID, from, to)` | Earnings per pay rule: hourly, per project, per assignment (day or job), flat per period; paid absences and surcharges; `Reasons` splits the total by reason |
| `PersonalOverview(data, from, to, weekStart)` | The person's view over all employers, with hints on the Working Hours Act; refuses data of several people |
| `PublicHolidays(region, year)`, `Regions()`, `AddHolidayRule` | Public holidays of the German states and Austria, as data, and of parts of a state (see below) |

Public holidays of parts of a state: some days are kept only in some municipalities. A part has a code of its own
below the state's and every holiday of the state, plus or minus those days; the rule names its law in `Source`
(`PublicHoliday.Source`). The person or the organization picks the part, the library lists no municipalities.

| Region | Difference to the state | Source |
|---|---|---|
| `DE-BY-AUGSBURG` | + Augsburg Peace Festival (8 August) | Art. 1 Abs. 2 FTG (Bavaria) |
| `DE-BY-PROTESTANT` | − Assumption Day (`DE-BY` counts it, as most Bavarian municipalities have a Catholic majority) | Art. 1 Abs. 1 Nr. 2, Abs. 3 FTG |
| `DE-SN-CATHOLIC` | + Corpus Christi | § 1 Abs. 1 SächsSFG with the Fronleichnamsverordnung |
| `DE-TH-CATHOLIC` | + Corpus Christi | § 2 Abs. 2, § 10 Abs. 1 ThürFGtG |

`HolidayRule.Except` takes a day away from a part; `AddHolidayRule` adds parts of one's own.

Night shifts: `Employer.DaySplit` says which day a span past midnight counts for, for target, balance, earnings
and the overview: `start_day` (the default, the whole span on the day it starts) or `midnight` (each day its part,
with the real length on daylight saving nights: 22:00 to 06:00 is 9 hours in late October, 7 in late March). Keeping
the start day, a timed break after midnight inside the shift still counts for the shift's day.

Pay beyond time worked, both with a valid-from date per employer; `ComputeEarnings` lists them as lines of their own
(`Basis` `paid_absence` or `surcharge`, `Reason` the absence type or surcharge kind), and `Earnings.Reasons` sums
by basis and reason:

- `AbsencePay` pays an absence type (or `public_holiday`, the target lost to a holiday): `target` pays the minutes
  of the schedule at the hourly rule (the minutes lost), `average` pays them at the average earnings per worked minute
  of the `Weeks` (13 by default) before the absence; without a target the average worked day counts. An absence type
  without a rule is unpaid; a monthly salary needs no rule. `EarningsDataFrom` says how far back to load for averages.
- `Surcharge` adds `BasisPoints` (2500 is 25 %) on `night` minutes in a clock window (`WindowStart` to `WindowEnd`,
  minutes after midnight, wrapping over midnight), on `sunday` and `holiday` minutes (a full holiday of the region or
  an own full holiday), and on the `overtime` of each period of the overtime basis. Night adds up with Sunday and
  holiday; on a Sunday that is a holiday, a holiday surcharge replaces the Sunday one. A minute counts by its own clock
  time, whatever day its entry counts for. The base is the minute's hourly rate, or `BaseAmount` for salaried staff.

Pay: `ApplyRate` stores the rate as a snapshot on an entry, `Revalue` lists entries whose snapshot differs from the
current rule (store them with `UpdateEntry` to re-value on purpose).

## Free text

`Parse(line, today)` reads lines such as `2h #meeting @ACME/Website note`, `8-16:30 30m Pause`, `Fr -2h früher weg`,
`Mo-Mi Urlaub`, `24.12. halb Urlaub`. A date only counts as the first word. `Parsed.Entries(opts)` makes entries (spans
are placed in the time zone, so daylight saving changes keep the real length), `Parsed.AbsenceRecord` an absence. The
caller resolves `Parsed.Project` to a project ID.

## Organizations and rights

- `Join(org, person, role, template, from)` makes the membership, the managed employer and the schedule from a
  template; `EndMembership` ends it and archives the employer, the records stay with the organization.
- `ActorFor(person, organization, memberships, today)` and `MayChangeSettings`, `MayBook`, `MayErase` decide who may
  do what. The store does not know who acts: ask these before calling it.
- `Store.Managed(ctx, organization, from, to)` is the only way an organization reads data: its managed employers and
  the records bound to them, never anything private.

## Approval (company feature)

Only for an organization with `Features.Approval` on; without it there are no submissions and nothing changes.

- `Submit(org, data, SubmitRequest)` makes a `Submission` of a managed employer: `period` (the days `From` to `To`,
  with target, actual and balance at handing in; no overlap with another binding period), `leave` (a requested
  absence) or `correction` (asks to open an approved submission again; `Note` says why). The caller stores it.
- `Decide(sub, employer, actor, approve, reason, at)`: an administrator of the organization, never on their own
  submission (`MayDecide`); a rejection needs a reason. `Withdraw(sub, person, at)` while it is open, by the person.
- Locks, in both stores: the days of a period that is handed in or approved refuse entries and absences
  (`Catalog.Locked`, `Catalog.AbsenceLocked`); a leave request locks its absence, and after its approval only the
  decision itself is written (`LeaveDecision` then `UpdateAbsence`; `Catalog.LeaveLocked`). An approved correction
  opens the period or the absence again; the person changes it and hands it in anew.
- A submission is replaced only while it is open and never deleted (`ErrLocked`); decided ones stay as they are.
- `ReviewData(org, data)` is what a reviewer sees of `Store.Managed`: the days handed in and the absences asked for,
  nothing else.
- `LeaveReminders(data, employerID, today, warnDays)`: open leave of the year within `warnDays` (92 by default)
  before 31 December, with whether the employer recorded its notice (`LeaveNotice`, BAG 9 AZR 266/20). The person
  sees it as a reminder, the organization on its managed data as the notices it still owes.

## Access log

`AccessLog` (both stores have it; it is separate from `Store`, so a store of one's own keeps working) records who
(`Actor`, for `Organization`) read, changed, exported or decided what of whose data (`Subject`), and when.

- `ReadManaged(ctx, store, log, actor, organization, from, to, purpose)` is `Store.Managed` with a read logged for
  every person whose records the organization gets. Changes, exports, decisions and role changes are logged with
  `LogAccess`.
- `Accesses(ctx, subject)` is the person's own view; `ExportOwner` includes it.
- Every entry holds the hash of the subject's entry before it (`Prev`, `Hash`); `VerifyAccesses` checks the chain.
  PostgreSQL only adds rows (no update policy) and only lets the retention purge (`ClassAccessLog`) delete rows older
  than two years; the in-memory store follows the same rule. The chain shows a changed single entry, not a rewritten
  chain: keep the last hash elsewhere if that matters.
- `Store.Erase` leaves the access log to its retention.

## Personal rights and retention

- `ExportOwner` collects everything of a person (JSON with `WriteJSON`, entries as CSV with `WriteEntriesCSV`).
  `Export.PrivatePart` and `ImportPrivate` take the private part to another instance.
- `Store.Erase(owner)` removes everything private of a person; managed records stay.
- `Retention` (`DefaultRetention`: three years after the end of the year, at least two; classes `time_records`,
  `leave_notices`, `access_log`) gives the cutoff for
  `Store.Purge(ctx, owner, class, cutoff)`, which skips employers on hold and logs every run.

## PostgreSQL

```go
o := pgstore.Options{Prefix: "time_", TenantSetting: "underdoc.tenant"}
steps, _ := pgstore.Migrations(o) // plain SQL, run each once in your own migration flow
store, _ := pgstore.New(pool, o)  // pool: its connections carry the tenant setting
```

A standalone program uses `pgstore.Migrate(ctx, conn, o)` (own version table `<prefix>schema_migrations`) and
`pgstore.Connect(ctx, url, o, tenant)`. Every table has a tenant column filled from the setting and row-level security
on it, also for the owner of the tables; a connection without the setting sees and writes nothing. Run the store as a
role without `BYPASSRLS` and not as a superuser, or the rules do not hold.
