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

## The list

The start page (`list.go`, `templates/list.html`) is the list of one employer: the week before and the week shown
side by side (`lg:grid-cols-2`, one above the other on a phone), each a form with a row per day. A row's field takes
hours or a short code; its pattern `dayPattern` follows `parseDayValue` (hours without a sign, or
`core.ParseDayCode`), and a test keeps them in step. Only rows whose field or note changed are written (`was-` and
`wasn-` carry what the page showed): hours become the day's one manual entry, a code an absence of that day alone,
and whatever the row booked before goes through the history. A day handed in for approval, an absence of several days
or of every employer, and a requested one show as text instead of a field. The first empty working day of the week
shown gets `autofocus`, so Enter after a number leads to the next day. `core.Ledger` gives the sums and the balance,
`core.WeekGaps` the notices on top (the last eight weeks), and the grids show the codes of the month and the year.
Corrections of the time account (`POST /book/correction`, `/corrections/{id}/remove`) are for whoever may change the
employer's settings (`core.MayChangeSettings`). Everything else of a mode stays on `/week`.

`/weeks` lists every week with the same template (`list-week` in `layout.html`), the newest at the bottom. `app.js`
scrolls it to the bottom and, when the link at the top comes into view, fetches the eight weeks before
(`part=1` answers only the part, `weeks-part`) and puts them above, keeping the view where it was; the link itself
opens the page from an older week on. Each week's form returns to its own week (`from=…#week-…`).

A day of a week handed in shows as text. A day of an approved week keeps its field where the organization approves
weeks: what is typed there goes, with the reason the week's footer asks for, into a change request (a correction
with `Proposal`; an open one takes further days in). The row shows the approved value and the value asked for; the
organization's page lists the days asked for. Approving it books the values in the administrator's name
(`applyChange`), hands the period in anew approved by the same decision, and logs the change in the member's access
log; a rejection changes nothing.
## Leave

`/leave` (`leave.go`) shows the leave account of a year as of today (`core.LeaveAt`) and the year as twelve month
grids. A free working day (a target in the schedule, or Monday to Friday without one; no absence, no public holiday,
not handed in) carries a checkbox; `POST /leave/plan` books the marked days as leave through `createAbsence`, so an
organization that approves leave gets a request at once. Marked days with only free days between them make one
absence. On the organization's page a leave request names the other members away on its days (`core.Overlaps` on
the data read for the reviews, leave types only).

## Projects of a day, weekly report

A row's weekday links to the day (`day.go`, `/day`): its bookings on projects or clients with order numbers, a form
to book more (`POST /book/project-day`; a project is found by name and number, or made), and what the typed hours
leave unbooked. The list shows that rest per day and week. `/report/week` renders a week to print (`view.Print`: no
header, tab bar or footer, the page inside C22's `print-sheet`), with a button that calls the browser's print.

## All employers, income, details of a day

`/all` (`all.go`) shows a week of every employer: a column per employer active in the week, a cell only on its
active days (hours, a code or an income), the earnings of each day (`core.ComputeEarnings` per employer and day, in
the currency of the first employer's pay), the week's sum, the average working day and what was taken in cash or by
card. The day page books incomes (`POST /book/income`, `/incomes/{id}/remove`) and the details of the day
(`POST /book/dayinfo`; all fields empty removes them).

## Company features, access log, pay

- Approval (`approval.go`): when an organization switches it on, a member hands in a week with a tap; leave (and
  absence types that need approval) is booked as requested and handed in at once. Handed-in days show as such and
  refuse changes; a correction with a reason opens an approved week again once approved. Administrators decide on the
  organization's page, never their own (an administrator's own request shows as such, without the buttons), and a
  rejection needs a reason. The days read for the reviews follow the open requests (`core.ReviewRange`), however far
  from today they lie. The rules are the core library's
  (`core.Submit`, `core.Decide`, `core.Withdraw`, `core.LeaveDecision`, `core.ReviewData`, `core.ReviewRange`).
- Access log: every read of an organization goes through `core.ReadManaged`, the organization's page through
  `core.ReadManagedDaily`, which logs a view once per member and day (in the organization's time zone) instead of on
  every call; decisions, membership and role changes,
  and an administrator's reads and changes of a member's settings go through `LogAccess`. The person sees the log on
  `/access` (linked from the reports), with a note whether the chain holds (`core.VerifyAccesses`). `Options.AccessLog`
  defaults to the store's own.
- Leave reminders: the week view shows `core.LeaveReminders` for the person while the year's data are loaded (the
  week shown lies in this year or next to it), with the employer's notice when there is one; the note on lapsing
  leave is only given for German employers (`core.LeaveNoticeApplies`).
- Notices on lapsing leave: the settings of a German employer list its `core.LeaveNotice`s and take a new one
  (`POST /employers/{id}/notice`, checked by `core.MayRecordLeaveNotice`: the person on a private employer, an
  administrator of the organization on a managed one; the day not after today, kept as noon UTC). Within the last
  92 days of the year the organization's page lists the notices it still owes (`core.NoticesOwed`) with the same form;
  the year read for it goes through `core.ReadManagedDaily` ("leave notices"), and a recorded notice into the member's
  access log as a change.
- Legal templates: `/legal` shows the files of `docs/legal` (embedded by the package `docs`) with their downloads
  and the kinds of data from `core.DataInventory`, with `docs/data-inventory.json` to download; the page says first
  that none of it is legal advice or checked by a lawyer.
- Pay beyond time worked: the employer settings take paid absences (`core.AbsencePay`) and surcharges
  (`core.Surcharge`), and how shifts past midnight count (`Employer.DaySplit`); the reports name every line by its
  reason.

