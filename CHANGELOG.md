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
