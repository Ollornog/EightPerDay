# The web interface

Server-rendered pages (`templates/`, Go `html/template`) in the design system [C22](https://github.com/Ollornog/C22), a
little JavaScript of its own (`assets/app/app.js`: dialogs, the question before removing, the theme, the passkey steps;
`sw.js`: the service worker), and nothing loaded from anywhere else. No package from npm is needed, so there is no
lockfile and no build step for JavaScript.

- `assets/c22/` is C22's build output, refreshed only by `scripts/vendor-c22.sh <commit>`; `VERSION` names the commit.
- `assets/licenses/THIRD-PARTY.txt` comes from `scripts/third-party-licenses.sh`; `scripts/check.sh` fails when it does
  not match the modules of the binary.
- `i18n/en.json` and `i18n/de.json` hold every text; a test keeps their keys and placeholders in step. Regions of
  public holidays are named there as `region.<code>` (`regions.go` orders them), and a test fails on a region of
  `core.Regions` without a name.
- `auth/` holds the accounts; `browsertest/` runs the program in a real Chrome (`go test -tags browser`).

## Everything from C22

The app has no stylesheet of its own: every class is one of C22's, and a test (`TestEveryClassExistsInTheStylesheet`)
fails on a class C22 lacks or on any CSS file in `assets/app/`. What the app needs and C22 lacks goes to C22 first.
Parts used beyond the plain components:

| Part | Where |
|---|---|
| Tab bar (`nav.tab-bar`, the open page `aria-current="page"`; the page keeps `pb-20` below its content) | `layout.html` |
| QR code (`img.qr-code`) and key to type off (`code.secret-key`) | setting up an authenticator app |
| Code block with wrapped lines (`pre.code-block[data-wrap]` with `max-h-96`) | the licenses page |
| Collapsible card (`details.card`, `<summary><h3>`, `<section>`) | absence, span |
| Blocks of the category "Data entry": day row, stat row, duration field | the week views |

A duration field carries `pattern` from `durationPattern` (Go, `track.go`), which a test keeps in step with
`parseHours`: the browser refuses early what the server would refuse, and nothing it would take.

## Company features, access log, pay

- Approval (`approval.go`): when an organization switches it on, a member hands in a week with a tap; leave (and
  absence types that need approval) is booked as requested and handed in at once. Handed-in days show as such and
  refuse changes; a correction with a reason opens an approved week again once approved. Administrators decide on the
  organization's page, never their own, and a rejection needs a reason. The rules are the core library's
  (`core.Submit`, `core.Decide`, `core.Withdraw`, `core.LeaveDecision`, `core.ReviewData`).
- Access log: every read of an organization goes through `core.ReadManaged`; decisions, membership and role changes,
  and an administrator's reads and changes of a member's settings go through `LogAccess`. The person sees the log on
  `/access` (linked from the reports), with a note whether the chain holds (`core.VerifyAccesses`). `Options.AccessLog`
  defaults to the store's own.
- Pay beyond time worked: the employer settings take paid absences (`core.AbsencePay`) and surcharges
  (`core.Surcharge`), and how shifts past midnight count (`Employer.DaySplit`); the reports name every line by its
  reason.

