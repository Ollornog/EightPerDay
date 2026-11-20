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
| `LeaveAt(data, employerID, year, today)` | `LeaveStatus`: the leave account as of a day: the entitlement earned up to it (aliquot, half days), approved leave taken up to it and planned after it, requested, remaining |
| `Overlaps(data, absence)` | Leave of other people on days of an absence, for a reviewer: types that count against leave only (never sickness), approved or requested |
| `ComputeEarnings(data, employerID, from, to)` | Earnings per pay rule: hourly, per project, per assignment (day or job), flat per period; paid absences and surcharges; `Reasons` splits the total by reason |
| `PersonalOverview(data, from, to, weekStart)` | The person's view over all employers, with hints on the Working Hours Act; refuses data of several people. The rest between two working days reads the spans with start and end and the spans typed for the hours of a day (`Entry.Typed`, `TypedSpans`) |
| `Ledger(data, employerID, start, from, to, until)` | `[]LedgerWeek`: the weeks of a list with their days, sums up to `until`, the corrections dated in them and the balance carried from week to week since `start`; its last balance equals `Overtime(…, start, until)` |
| `WeekGaps(data, employerID, start, from, today, approval)` | `[]WeekGap`: weeks that lack something, for notices: working days before today without any booking, a plan week over and not confirmed, a week over and not handed in where the organization approves weeks |
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

Hours typed for a day (`DayTotal`: work by the day, by hand, without span or project) hold the bookings on projects
of the same day by the day: the day counts the larger of both, and `Day.Unbooked` (summed in `Total.Unbooked`) is
what the projects leave of the typed hours. Without typed hours the bookings on projects make the day; a free-text
line or a span still adds. `Project.Number` holds an order or job number.

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

Income and the details of a day, two catalog kinds per employer and day:

- `Income` (kind `income`): money earned beyond the pay rules: the amount of a job with its client (a flight, a
  lesson), commission, photos, tips (`IncomeKind`), and how it was taken (`PaidBy`: cash, card, transfer).
  `ComputeEarnings` lists incomes as lines of the basis `income` with the kind as reason, and `Earnings.Taken` sums
  them by how they were taken.
- `DayInfo` (kind `day_info`, ID `DayInfoID(employer, day)`): place of work, kilometres one way and in all (tenths),
  standby passive and active (minutes) and the call-outs of active standby (`Callouts`).
- `PayRule.Salaries`: a monthly `flat_period` rule with 13 or 14 salaries a year pays the extra months spread over
  the year (0 means 12).

Half days and standby (ADR-3):

- `Entry.Part` marks work of half a day, `am` (`Morning`) or `pm` (`Afternoon`). `Day.Part` is the half when every
  work entry of the day marks the same one; a morning and an afternoon, or any work without a half, make the day
  whole. A `per_assignment` rule per day pays a half day at `PayRule.HalfAmount`, or half of `Amount` without one
  (`HalfDayAmount`); the line has `HalfDay` set.
- Standby is paid by `PayRule`s of the bases `standby_passive` and `standby_active`: no unit pays the minutes per
  hour, `day` pays each day with standby of the kind, `assignment` (active only) each call-out (`DayInfo.Callouts`,
  at least one when there are active minutes). Lines carry the basis as reason and the rule's `Unit`.
- `Employer.StandbyWork` says which standby is working time for the time account: `active` (the default, empty),
  `none` or `all`. `Day.Standby` is that part, and `Day.Worked` holds it, so balance, overtime and the overview of the
  Working Hours Act count it. Pay for hours, pay per day and the average of paid leave leave it out: standby is paid
  by its own rules only.
- One currency per instance and employer, as before; several currencies are not planned (ADR-3).

Pay: `ApplyRate` stores the rate as a snapshot on an entry, `Revalue` lists entries whose snapshot differs from the
current rule (store them with `UpdateEntry` to re-value on purpose).

## Free text

`Parse(line, today)` reads lines such as `2h #meeting @ACME/Website note`, `8-16:30 30m Pause`, `Fr -2h früher weg`,
`Mo-Mi Urlaub`, `24.12. halb Urlaub`. A date only counts as the first word. `Parsed.Entries(opts)` makes entries (spans
are placed in the time zone, so daylight saving changes keep the real length), `Parsed.AbsenceRecord` an absence. The
caller resolves `Parsed.Project` to a project ID.

## Short codes

`ParseDayCode(input)` reads what people type into a day of a spreadsheet instead of hours: `U`/`V` leave, `K`/`S` sick,
`F`/`O` a day off, `ZA`/`C` time off in lieu, in upper or lower case, half days with `½`, `/2` or `0,5` in front or
behind (`½U`, `U/2`). Public holidays have no code: they come from the region. `CodedAbsence(key)` says whether an
absence type has one.

## The line of a day

`ParseDay(input, DayOptions)` reads everything a person did on one day typed into one field: Parse's language for one
day (durations, a span with its break, `#tags`, `@project`, the rest a note) with plain numbers as hours (`8`, `8,5`,
`7:30`), the short codes of `ParseDayCode`, the half of a day behind the hours (`4v`, `4n`, `4a`, `4p`, `4 VM`,
`4 NM`, `4 AM`, `4 PM`: `Entry.Part`), half a day of a code next to hours (`½U 2,5n`), the key of an employer in front
of the hours (`f3`, `nU`) and a word that names a project behind them (`f3 studio`, by its order number, its name or
the beginning of a word of its name with three letters at least: `MatchProject`). Values come first; behind the first
word of the note only hours with a key count. `DayLine.Groups` holds one `DayGroup` per employer and project
(`Minutes`, `Part`, `Code`, `Typed` for a span with its break as typed, `Tags`), `DayLine.Note` the rest. It refuses
a project without hours in front of it, a code on a project, two codes for one employer's day, hours next to a whole
day of absence and more than 24 hours.

`DayOptions.Default` is the employer of hours without a key, `Keys` maps keys to employers (`EmployerKeys(employers)`
gives every employer one: `Employer.Key`, else the first free letter of its name, then two letters, then any letter),
`Projects` the projects a word may name. `ProjectWord(p)` writes a project back so that it reads the same.
`TypedSpans(typed)` reads the spans back out of what was typed (`8-16:30 30m` is 08:00 to 16:30; `22-6` ends the next
morning, `ClockSpan.End` past 24:00).

A proposed day of a change request (`ProposedDay`) holds the same: minutes with `Part` and `Typed`, next to half a day
of an absence minutes, the note of the day, and the hours on projects (`Projects`, `ProposedProject`).

## Preferences

`Preferences` (catalog kind `preferences`, one record per person whose ID is the person's) keeps how the person likes
the interface: the view of the start page, its detail and the employers whose projects stand open. The library keeps
the words only; `Catalog.PreferencesOf(owner)` returns them, or empty ones. They travel with the private part of an
export and are erased with it.

## Organizations and rights

- `Join(org, person, role, template, from)` makes the membership, the managed employer and the schedule from a
  template; `EndMembership` ends it and archives the employer, the records stay with the organization.
- `ActorFor(person, organization, memberships, today)` and `MayChangeSettings`, `MayBook`, `MayErase`,
  `MayRecordLeaveNotice` decide who may do what. The store does not know who acts: ask these before calling it.
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
- A change request is a correction that carries the values it asks for (`Submission.Proposal`, `ProposedDay`: hours
  with the span typed for them or an absence with a short code, the note of the day, and the hours of the projects
  that change, per day of the approved period; `SubmitRequest.Proposal`). Until it is
  decided the period stays binding and its approved values count. Approved, it opens the period like any correction;
  the caller then books the proposed values and hands the period in anew, approved by the same decision (the
  standalone program does both in one step). `Catalog.PeriodAt` gives the binding period of a day,
  `Catalog.OpenCorrection` the open correction of a submission.
- `ReviewData(org, data)` is what a reviewer sees of `Store.Managed`: the days handed in and the absences asked for,
  nothing else. `ReviewRange(catalog, today)` gives the days to read for it: from the first to the last day of any
  open submission, and today, so a leave request far ahead or a correction of an old week comes with its records.
- `LeaveReminders(data, employerID, today, warnDays)`: open leave of the year within `warnDays` (92 by default)
  before 31 December, with whether the employer recorded its notice (`LeaveNotice`, BAG 9 AZR 266/20). The person
  sees it as a reminder, the organization on its managed data as the notices it still owes.
- `LeaveNoticeApplies(employer)`: the notice matters for an employment (kind `employer`) that is German by its region
  (`DE`, `DE-…`) or names none; not for Austria, clients or private work. `MayRecordLeaveNotice(employer, actor)`:
  whoever stands for the employer records it, the person on a private employer, an administrator on a managed one.
- `NoticesOwed(data, today, warnDays)`: for every active employer in the data the notice applies to, the reminder of
  open leave without a notice for the year, ordered by employer; on `Store.Managed` of the whole year, the notices an
  organization still owes. `LeaveNotice.Text` holds at most 2000 characters.

## Access log

`AccessLog` (both stores have it; it is separate from `Store`, so a store of one's own keeps working) records who
(`Actor`, for `Organization`) read, changed, exported or decided what of whose data (`Subject`), and when.

- `ReadManaged(ctx, store, log, actor, organization, from, to, purpose)` is `Store.Managed` with a read logged for
  every person whose records the organization gets. Changes, exports, decisions and role changes are logged with
  `LogAccess`.
- For a view that is opened again and again, `ReadManagedDaily(…, purpose, at, loc)` logs with
  `LogAccessOnce(ctx, log, access, loc)`: nothing is added while the person's log already holds an entry of the same
  day (in `loc`) by the same actor and organization, for the same employer, action and purpose, whose days cover the
  new read. Other days, another purpose, another actor or the next day each add an entry, so nothing read goes
  unlogged; entries are still only added, and the chain and `VerifyAccesses` are unchanged. Added in a
  backwards-compatible way: `ReadManaged` and `LogAccess` behave as before.
- `Accesses(ctx, subject)` is the person's own view; `ExportOwner` includes it.
- Every entry holds the hash of the subject's entry before it (`Prev`, `Hash`); `VerifyAccesses` checks the chain.
  PostgreSQL only adds rows (no update policy) and only lets the retention purge (`ClassAccessLog`) delete rows older
  than two years; the in-memory store follows the same rule. The chain shows a changed single entry, not a rewritten
  chain: keep the last hash elsewhere if that matters.
- `Store.Erase` leaves the access log to its retention.

## Stopwatches

`Stopwatches` keeps the running stopwatch of each person (`Timer`: employer, project, note, start; at most one per
person, `ErrTimerRunning` on a second start). Both stores of the library have it, separate from `Store`; the standalone
program keeps its stopwatches next to its accounts, an embedding application in the store, inside its tenant.
`Store.Erase` removes a person's stopwatch.

## Calendar

`CalendarAbsences(data, from, to)` lists the absences of a range for a calendar (UnderDoc's M-18): who, which employer,
the type with its name, the days, half days, the status, whether it counts as leave and whether the type reveals health
data, without notes, rejected or deleted ones. It works on a person's data (`Load`) and on `Store.Managed`; the caller
decides who sees which, and shows a type with `Health` to others only as away.

## Personal rights and retention

- `ExportOwner` collects everything of a person (JSON with `WriteJSON`, entries as CSV with `WriteEntriesCSV`).
  `Export.PrivatePart` and `ImportPrivate` take the private part to another instance.
- `Store.Erase(owner)` removes everything private of a person; managed records stay.
- `Retention` (`DefaultRetention`: three years after the end of the year, at least two; classes `time_records`,
  `leave_notices`, `access_log`) gives the cutoff for
  `Store.Purge(ctx, owner, class, cutoff)`, which skips employers on hold and logs every run.

## Data inventory

`DataInventory()` lists every record the library stores with its purpose, holder, deletion and every field with its
purpose (A21). [`data-inventory.json`](data-inventory.json) is its output; a test fails when a field has no
description or the file is out of date (`go test ./core -run TestDataInventory -update-inventory` rewrites it).
Templates for employers (a works agreement skeleton, an entry for the record of processing activities) are in
[`legal/`](legal/README.md), as drafts and without legal advice.

## PostgreSQL

```go
o := pgstore.Options{Prefix: "time_", TenantSetting: "underdoc.tenant"}
steps, _ := pgstore.Migrations(o) // plain SQL, run each once in your own migration flow
store, _ := pgstore.New(pool, o)  // pool: its connections carry the tenant setting
```

`TestNoTenantReachesAnother` (in `core/pgstore`) proves for every exported method of the store that no tenant reads or
changes a row of another and that a connection without a tenant reads and writes nothing; a method added later fails
it until it is covered.

The access log has one permissive policy that keeps to the tenant, like every table, and restrictive ones on top: no
row changes, and a row goes only after two years, or when the application deletes a whole tenant for good and sets
`o.EraseSetting()` (the tenant setting with `_erase`, such as `underdoc.tenant_erase`) to that tenant within the
transaction of the deletion.

A standalone program uses `pgstore.Migrate(ctx, conn, o)` (own version table `<prefix>schema_migrations`) and
`pgstore.Connect(ctx, url, o, tenant)`. Every table has a tenant column filled from the setting and row-level security
on it, also for the owner of the tables; a connection without the setting sees and writes nothing. Run the store as a
role without `BYPASSRLS` and not as a superuser, or the rules do not hold.

## Embedding the interface

`web.New` with `Options.Embedding` runs the web interface of the standalone program inside another application:

```go
h, _ := web.New(log, web.Options{Store: store, Embedding: &web.Embedding{
	Base:      "/time",                         // where the application mounts the handler
	Account:   func(r *http.Request) *auth.Account { /* from the application's sign-in, nil for nobody */ },
	Directory: accounts,                        // Account(ctx, id) and Accounts(ctx) of the application
	ThemeKey:  "app.theme",                     // localStorage key of the application's theme
}})
mux.Handle("/time/", h)
```

- The ID of the account is the person in the store; its `Language`, when set, is the language of the pages.
- No login, setup, account or admin page, no service worker and no organization founded through the interface: the
  application maps its tenant to an organization and its administrators to memberships itself
  (`Store.PutOrganization`, `Store.PutMembership`); in the interface the tab "Organizations" replaces "Account".
- Every link, form and redirect stays below `Base`. Changes are accepted only from pages of the request's own host
  (`Sec-Fetch-Site: same-origin` or a matching `Origin`), and the pages may be framed only by the same origin
  (`frame-ancestors 'self'`, `X-Frame-Options: SAMEORIGIN`).
- The store passed in must carry the tenant of the request in its connections (an application's pool that sets the
  tenant setting per request); stopwatches come from the store unless `Options.Timers` names others.
