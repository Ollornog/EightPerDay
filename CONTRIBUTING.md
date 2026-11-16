# Contributing

<b>English</b> · <a href="i18n/CONTRIBUTING.de.md">Deutsch</a>
<br /><br />

Thanks for taking the time. This is a small project, so the rules are short.

## Before you write code

**Open an issue first** for anything beyond a typo or an obvious bug. The reasoning behind the design lives in
[`backlog/`](backlog/) as decision records, so please read the relevant one before proposing to change it.

**Bring your own code.** EightPerDay shares goals with other time trackers, not their code. Do not copy or port code,
texts, assets or translations from them, in particular not from projects under the AGPL, GPL or SSPL.

## Workflow

- Work on a **feature branch**, not on `main`.
- Run the full suite before you push: `scripts/check.sh`. It must be green **twice in a row**. It needs Go, Python 3
  and the PostgreSQL server binaries (`initdb`, `pg_ctl`, `postgres`; found through `PG_BIN` or under
  `/usr/lib/postgresql/<version>/bin`).
- Documentation and `CHANGELOG.md` travel in the **same commit** as the change they describe.
- Code, comments, commits and docs are in English. German exists as a translation only.
- Examples use `example.com` and made-up names, never real companies, hosts or data.

## What the suite checks

Besides the Go tests, the suite enforces repository hygiene: required files, pinned GitHub Actions, workflow
permissions, changelog structure, and that the German and English documents keep the same shape. If hygiene fails,
fix the cause instead of working around the check.

## Calculations

Every rule that computes a number (target, actual, balance, leave, earnings) needs a table test, including month ends,
the change to and from daylight saving time and a schedule that changes in the middle of a month. A fix comes with a
test that fails without it.

## Code of Conduct

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

<br /><br />
<p align="right"><img src="assets/logo.png" alt="EightPerDay" width="60" height="60"></p>
