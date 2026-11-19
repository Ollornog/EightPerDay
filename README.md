<p align="center"><img src="assets/logo.png" width="250" height="250" alt="EightPerDay logo, the number eight in a yellow circle"></p>
<h1 align="center">EightPerDay</h1>

<p align="center"><b>English</b> · <a href="i18n/README.de.md">Deutsch</a></p>

**Time tracking that adapts to how you work, not the other way round.**

> **Status: early.** The core library is there ([docs/core-api.md](docs/core-api.md)), and the standalone program runs
> with every mode in its web interface; no release yet, see [`backlog/`](backlog/).

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

## Running

One binary, PostgreSQL, nothing else; the web interface is built for phones first and can be installed to the home
screen. [`compose.yaml`](compose.yaml) is an example with the database. Configuration comes from the environment:

| Variable | Meaning |
|---|---|
| `EIGHTPERDAY_DATABASE_URL` (or `…_FILE`) | PostgreSQL connection string, required |
| `EIGHTPERDAY_ORIGIN` | the address people open, such as `https://example.org`; passkeys are bound to its host name |
| `EIGHTPERDAY_ADDR`, `EIGHTPERDAY_HEALTH_ADDR` | where the interface and the health endpoint listen (`127.0.0.1:8080`, `127.0.0.1:8081`) |
| `EIGHTPERDAY_TRUSTED_PROXIES` | networks whose `X-Forwarded-For` and gate headers are believed |
| `EIGHTPERDAY_GATE_USER_HEADER` and `…_NAME_HEADER`, `…_GROUPS_HEADER`, `…_ADMIN_GROUP` | optional: trust a sign-in gate in front |

**Using it.** Each person adds employers, clients or private work and picks per employer how exactly to track; the week
view follows the mode: one field for the weekly total, hours per day against the schedule, the plan with quick
buttons for leave, sickness and deviations and "the week is right", a stopwatch and spans for exact time, or hours per
project. A free-text line (`8-16:30 30m Pause`, `Fr -2h früher weg`, `Mo-Mi Urlaub`) works in every view and says
what it understood while it is typed. Reports show balance, time account, leave and earnings per employer and month,
and the person's own hints on the Working Hours Act; everything about a person can be exported as JSON or CSV.
Organizations manage members, schedule templates and pay; they see only what belongs to them, never a member's
private employers, and their company features are off unless switched on with a works agreement. Towards the end of
the year the week view reminds a person of leave still open; the page `/legal` shows the templates for employers from
[`docs/legal`](docs/legal/) and the kinds of data stored, as drafts that are no legal advice.

**Signing in.** There is no sign-up. The first account, which administers the program, comes from a one-time link made
at the server's console with `eightperday link --admin` (in a container `docker compose exec eightperday /eightperday
link --admin`): it lasts one hour, a new one replaces it, and it never goes to the log, which only says how to make
it. After that `eightperday link [--admin] [<name>]` prints an invitation at the console, and administrators invite
people in the interface. Each person chooses a passkey (the default), a password with an
authenticator app, or a password alone. Passwords are hashed with Argon2id, failed sign-ins are slowed down per name and
per address, and sessions live in HTTP-only cookies. Behind a sign-in gate, its headers can sign people in instead, but
only from the trusted proxies.

## Development

`scripts/check.sh` runs everything: repository hygiene, gofmt, go vet, staticcheck, govulncheck, the tests with the
race detector, and a browser test in a real Chrome on the screen of a phone. It needs Go, Python 3, the PostgreSQL
server binaries and Chrome. `scripts/check-image.sh` checks the container image. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

The code is under the [MIT License](LICENSE).

The logo is **not** under the MIT License: Icon by [Alfredo Creates](https://www.flaticon.com/authors/alfredo-creates) from
[Flaticon](https://www.flaticon.com/), used under the Flaticon license with attribution, see
[assets/LICENSE-icon.md](assets/LICENSE-icon.md).
