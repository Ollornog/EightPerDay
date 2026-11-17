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
| `core/storetest` | The conformance test every `Store` passes; run it for a store of your own |

## Model

- `Organization` (members, administrators, schedule templates, retention, company features) and `Membership`
  (person, role, period, the managed employer).
- `Employer` with `Holder` `private` or `managed` (+ `Organization`), `Mode`, time zone, holiday region.
- Catalog records, filed by `Store.Put` and read as a whole with `Store.Catalog(owner)`: `ModeChange`, `Schedule`,
  `PayRule`, `Client`, `Project`, `Activity`, `Tag`, `AbsenceType`, `Confirmation`, `Adjustment`, `PeriodClose`,
  `Holiday`, `LeaveNotice`, `PurgeLog`.
- Time records with history: `Entry` (`work`, `break`, `adjust`; `Minutes` always, `Start`/`End` optional) and
  `Absence`. Every create, update and delete writes a `Revision`; updates need the current `Version`
  (`ErrConflict`), closed periods refuse changes (`ErrLocked`).

## Modes

`week_sum` (one total per week), `schedule` (target, balance), `plan` (the schedule counts as worked, only deviations
are booked as `adjust` entries, weeks are confirmed with a `Confirmation`), `exact` (spans and breaks per project),
`projects_only`. A `ModeChange` switches from a date on; nothing is rewritten. `MaterializePlan` turns plan days into
entries before a switch to `exact`; `SpreadWeek` splits a week entry into days.

## Calculations

All of them are pure functions over `core.Data` (catalog, entries, absences), loaded with `core.Load(ctx, store,
owner, from, to)`.

| Function | Result |
|---|---|
| `Days(data, employerID, from, to)` | `[]Day`: target, adjusted target, worked, breaks, credited, actual, balance, holiday, absences, minutes per project |
| `Sum(days)`, `Totals(days, PeriodWeek/Month/Year, weekStart)` | Totals per period |
| `Overtime(data, employerID, from, to)` | Time account: balance with adjustments, overtime by the schedule's basis (day, week, month) |
| `Leave(data, employerID, year)` | Leave account: entitlement (prorated), adjustments, taken, requested, remaining |
| `ComputeEarnings(data, employerID, from, to)` | Earnings per pay rule: hourly, per project, per assignment (day or job), flat per period |
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

## Personal rights and retention

- `ExportOwner` collects everything of a person (JSON with `WriteJSON`, entries as CSV with `WriteEntriesCSV`).
  `Export.PrivatePart` and `ImportPrivate` take the private part to another instance.
- `Store.Erase(owner)` removes everything private of a person; managed records stay.
- `Retention` (`DefaultRetention`: three years after the end of the year, at least two) gives the cutoff for
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
