<p align="center"><img src="assets/logo.png" width="250" height="250" alt="EightPerDay logo, a stopwatch"></p>
<h1 align="center">EightPerDay</h1>

<p align="center"><b>English</b> · <a href="i18n/README.de.md">Deutsch</a></p>

**Time tracking that adapts to how you work, not the other way round.**

> **Status: early.** The core library is there ([docs/core-api.md](docs/core-api.md)); the standalone program with its
> web interface is being built, see [`backlog/`](backlog/).

## Why

Most time trackers want every working day booked from start to end. Open source ones rarely know target hours, overtime
or leave at all, and none of them handle several employers or clients with their own schedules and pay. A craftsman who
only wants to note "sick on Wednesday" or a freelancer with three clients ends up with a spreadsheet.

## What it will do

- **Several employers and clients per person**, also private ones, each with its own schedule and its own pay.
- **Organizations** that manage the time tracking of their members, strictly separated from what a person tracks
  privately; a person can do both at once and gets one overview over everything.
- **Operating modes per employer, switchable at any time without losing data:**
  - *Weekly total*: one number per week, the next week starts empty.
  - *Schedule*: target hours per weekday, balance and overtime.
  - *Plan counts*: the schedule counts as worked, you only book what differs (sick, leave, longer, left early) and
    confirm the week.
  - *Exact*: start, end and breaks, split across projects.
  - *Projects only*: time per project, no total working time.
- **Schedules and pay rules with a valid-from date**, so changing them never rewrites the past.
- **Pay per hour, per project, per assignment or per period**, and earnings per employer and period.
- **Absences** (leave, sickness, public holidays and more), public holidays of the German states and Austria as data.
- **Free-text entry** such as `2h #meeting @Website ACME` or `8-16:30 30m break`.
- **Correction history instead of overwriting**, configurable retention and an export for the person concerned.

## How it is built

- A **Go library** (`core`) with the data model, the calculations, the parser and a storage interface, with a
  PostgreSQL implementation (embedded migrations, table prefix, tenant column with row-level security) and an
  in-memory one.
- A **standalone binary** with a small web interface on top of it.
- The library is meant to be embedded: another application can run the migrations in its own database and build its
  own interface. [UnderDoc](https://github.com/Ollornog/UnderDoc) does that.

Decisions are recorded in [`backlog/`](backlog/).

## Development

`scripts/check.sh` runs everything: repository hygiene, gofmt, go vet, staticcheck, govulncheck and the tests with the
race detector. It needs Go, Python 3 and the PostgreSQL server binaries. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

The code is under the [MIT License](LICENSE).

The logo is **not** under the MIT License: Icon by [juicy_fish](https://www.flaticon.com/authors/juicy-fish) from
[Flaticon](https://www.flaticon.com/), used under the Flaticon license with attribution, see
[assets/LICENSE-icon.md](assets/LICENSE-icon.md).
