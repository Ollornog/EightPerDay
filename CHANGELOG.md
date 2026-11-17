# Changelog

All notable changes to this project are documented in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The repository: MIT license, bilingual README (English, German), security policy, contributing guide, code of
  conduct, the shared test kit with its Go gate, hardened workflows (CI, vulnerability audit, release, Dependabot
  auto-merge), and the backlog with milestones M-1 to M-3 and the decisions ADR-1 (a Go library plus a standalone
  binary) and ADR-2 (the data model).
- The logo, a stopwatch by juicy_fish from Flaticon, with its own license note in `assets/LICENSE-icon.md`.
- The core library (`core`, M-1), documented in `docs/core-api.md`:
  - Data model of ADR-2: employers held privately or managed by an organization, organizations with members,
    administrators, schedule templates and company features (off by default), schedules and pay rules with a
    valid-from date, one entry table, absences with types that fill or reduce the target.
  - Five modes per employer (`week_sum`, `schedule`, `plan`, `exact`, `projects_only`), switchable without losing
    data; plan days become entries on the way to `exact`.
  - Calculations: target, actual and balance per day, week, month and year; overtime account by day, week or month;
    leave account; earnings per hour, project, assignment and period; public holidays of the German states and
    Austria as data; the person's overview over all employers with hints on the Working Hours Act.
  - A free-text parser (`8-16:30 30m Pause`, `Fr -2h früher weg`, `Mo-Mi Urlaub`) with a fuzz test.
  - Correction history instead of overwriting, closed periods, retention classes with purge and legal hold, export as
    JSON and CSV, erasure of the private part, import of the private part.
  - The storage interface with an in-memory store and a PostgreSQL store (embedded migrations with table prefix,
    tenant column, row-level security on a configurable setting), both held to one conformance test.
- The standalone program `cmd/eightperday` (M-2, part one): one binary with embedded assets, a strict CSP, a health
  endpoint, configuration from the environment, PostgreSQL, and a container image with its check
  (`scripts/check-image.sh`, `compose.yaml` as an example).
- Signing in without sign-up: the first account through a one-time link printed at the first start or by
  `eightperday link`, invitations and reset links from the administration; a passkey (the default), a password with an
  authenticator app, or a password alone; Argon2id, a throttle per name and address, sessions in HTTP-only cookies, and
  optionally the headers of a trusted sign-in gate.
- The web interface's frame in the design system C22 (vendored by `scripts/vendor-c22.sh`), phone first and
  installable, in English and German, with an about dialog that names the logo's author and a page with the licenses
  of everything inside the binary (`scripts/third-party-licenses.sh`).
- A browser test in a real Chrome on the screen of a phone, with touch input and a virtual passkey authenticator.
- The time tracking views (M-2, part two), on the core library and its PostgreSQL store: an employer switcher and a
  start page; per mode a week view (weekly total; hours per day with target and balance; the plan with leave, sickness,
  deviations, "the week is right" and the switch to exact for one week; a server-side stopwatch and spans for exact
  time; hours per project); a free-text line with a live preview; absences; employer settings with mode changes,
  schedules, pay rules and projects, each with a valid-from date; monthly reports with balance, time account, leave,
  earnings and the person's own hints on the Working Hours Act; export as JSON and CSV; organizations with members,
  roles, schedule templates and the presence view as a company feature that needs a works agreement and a
  confirmation.
- Public holidays of parts of a state, as data with their legal source: the Augsburg Peace Festival
  (`DE-BY-AUGSBURG`), Bavarian municipalities without Assumption Day (`DE-BY-PROTESTANT`), and Corpus Christi in
  the Catholic municipalities of Saxony (`DE-SN-CATHOLIC`) and Thuringia (`DE-TH-CATHOLIC`). Rules can now leave
  regions out (`HolidayRule.Except`).
- Night shifts: an employer counts a span past midnight for the day it starts (the default) or splits it at local
  midnight (`Employer.DaySplit`), with the real length on daylight saving nights; a break after midnight in a night
  shift counts for the shift's day. `Load` reads entries from the day before.
- Pay beyond time worked: paid absences (`AbsencePay`: public holidays, leave, sickness or own types, at the target
  minutes or at the average of the reference weeks) and surcharges (`Surcharge`: night window, Sunday, holiday,
  overtime, in basis points, valid from a date, with an hourly base for salaried staff). The earnings list them by
  reason (`EarningLine.Reason`, `Earnings.Reasons`).
- Approval as a company feature: periods and leave requests handed in (`Submit`), approved or rejected with a reason
  by an administrator who is not the person (`Decide`), withdrawn while open; handed-in and approved records are
  locked in both stores, a correction is a new request that opens them again once approved; a reviewer sees only what
  was handed in (`ReviewData`). Leave reminders before the end of the year with the employer's notice as a data
  point (`LeaveReminders`).
- An access log (`AccessLog`, A8): who read, changed, exported or decided whose data when, chained by hash per person
  (`VerifyAccesses`), visible to the person and part of the export; `ReadManaged` logs every read of an
  organization. In PostgreSQL (migration 002) rows can only be added, and only rows older than two years can be
  purged (retention class `access_log`).
- The data inventory (`DataInventory`, A21): every stored record and field with purpose, holder and deletion,
  generated into `docs/data-inventory.json` and held to the code by a test. Templates for employers in `docs/legal/`
  (works agreement skeleton, record of processing activities), marked as no legal advice and not checked by a lawyer.
- A weekly total can be split into days ("Split this week into days"): by the targets of the schedule, Monday to
  Friday evenly without one; from that week on the employer counts hours per day. Only who may change the settings may
  split.
- The web interface for the company features and pay: members hand in weeks and leave, administrators approve or
  reject them with a reason on the organization's page, handed-in days are marked and locked, an approved week can be
  opened again by a correction; a page for the person shows who of an organization read, changed or decided what of
  their data (the access log, also written for settings and memberships); the employer settings take paid absences,
  surcharges and how shifts past midnight count, and the reports name each earning by its reason.

### Changed

- The web interface takes everything from C22 (vendored at `e8153c2`): the tab bar, the QR code, the key of an
  authenticator app, the wrapped license texts and the collapsible cards are C22's own now, and `assets/app/app.css`
  is gone. The week views follow C22's data-entry blocks; duration fields tell the accepted forms and refuse other
  input already in the browser.
- The selects of a region of public holidays show readable names in English and German instead of codes: Germany
  first, each state followed by its parts (`DE-BY-AUGSBURG`, `DE-BY-PROTESTANT`, `DE-SN-CATHOLIC`,
  `DE-TH-CATHOLIC`), then Austria; a region an embedding adds keeps its code.

- The organization's page logs its reads once per member, view and day (`ReadManagedDaily`, `LogAccessOnce`, both
  new and backwards-compatible) instead of on every call; a read of other days still adds an entry, and the chain is
  unchanged.
- The reviews read the days of the open requests (`ReviewRange`, new) instead of a fixed span from 62 days back to
  366 days ahead, so a leave request far ahead or far back shows with its absence and can be decided.
- An administrator's own request shows on the organization's page as such ("another administrator decides it"),
  without the buttons to decide it.

### Security

- The one-time link for the first administrator no longer goes to the log, where everyone who reads the log could
  take the program over: a start without an account only says how to make it, and it is made at the server's console
  with `eightperday link --admin`. It lasts one hour instead of 24, and a new one replaces the one before.

### Fixed

- A form without a date (such as a pay rule or a surcharge) answers that the date is missing instead of failing.
- The browser test taps only once the page has come to rest; a section that still grew moved the touch elsewhere.
