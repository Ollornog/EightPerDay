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

## Inside another application

With `Options.Embedding` the same pages run inside another application (UnderDoc), below a path of its own, with its
sign-in and in its tenant ([docs/core-api.md](../docs/core-api.md#embedding-the-interface)). The templates write every
link as `{{base}}/…`; `embed.go` takes the prefix off each request (`mounted`) and puts it in front of every redirect,
so handlers only ever see paths from the root. `data-base` on `<body>` gives it to `app.js`; `data-theme-key` on
`<html>` tells `theme.js` where the application keeps its theme, which the frame then follows.

## Everything from C22

The app has no stylesheet of its own: every class is one of C22's, and a test (`TestEveryClassExistsInTheStylesheet`)
fails on a class C22 lacks or on any CSS file in `assets/app/`. What the app needs and C22 lacks goes to C22 first. The page loads C22's `c22.js` besides Basecoat's script; the question before
a form removes something is C22's dialog (`form[data-confirm]` with `data-confirm-variant="destructive"`, its words in
`data-confirm-title`, `-ok` and `-cancel` on `<html>`).
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

## The sheet

The start page (`sheet.go`, `sheet_save.go`, `templates/sheet.html`; [ADR-4](../backlog/ADR-4-views-of-one-view-model.md))
is one endless list of weeks for every employer of the person. It opens at this week (three weeks before it, two to plan
after it), `?from=…` opens it further up, and `?part=weeks&before=…` answers the eight weeks before a week as a part
(`sheet-part`), which `app.js` puts above when the link at the top comes into view, keeping the view where it was.

- **One view model, three views.** `buildSheet` reads the weeks: the employers with their keys (`core.EmployerKeys`),
  the columns of the detail, per day a field per column (`sheetCell`: shown value, typed value, placeholder, hours not
  booked on projects, fixed, the value a change request asks for), the sums per column, total, target, difference,
  balance (`core.Ledger` per employer with a schedule), earnings (`core.ComputeEarnings`) and the state of each
  employer's week. The table, the journal and the grid (`table`, `journal`, `grid` in the template) only render it.
- **Detail.** Automatic: hours for one employer without projects, its projects as columns once it has some, a column
  per employer for several (their projects open next to them, kept in the preferences). Hours only and projects show
  the first employer with a schedule and sum the others into the day (`data-others`). A day belongs to the first
  employer with a target that day: hours without a key and the note go there.
- **Fields.** A field has `data-x` (its column, or its day in the grid) and sits in a `data-line` (the row of a day, or
  of a column in the grid); `app.js` moves with Enter, Shift+Enter and the arrows along them and leaves Tab to the
  browser. A field shows its value (`value`) and gives back what was typed while it has the focus (`data-raw`, such as
  a span with its break, `Entry.Typed`). Leaving a changed field posts it to `POST /sheet/cell` (`date`, `col`:
  `e-<employer>`, `p-<project>`, `note` or `line`, `value`, `view`, `to`, and `reason` for a change request); the
  answer is JSON with the weeks from the one changed on, and the page takes over only the parts marked `data-slot`.
  A value the server refuses comes back as `error` (status 200) and stays in its field with `aria-invalid`; a change to
  an approved week answers `reason` and the page asks for it in a dialog. `data-saving` on the sheet is set while
  answers are due.
- **Without JavaScript** every week is a form (`POST /sheet/week`) for the fields that name it (`form="wf-…"`): `c:` is
  what a field says now and `w:` what the page showed, and only fields that changed are written.
- **The line of a day** (journal) is read by `core.ParseDay` with the keys and projects of the person; it is written
  back with the day's own employer first, other employers with their key, and a project by its word
  (`core.ProjectWord`). What the line no longer says goes; employers whose day is handed in keep it.
- **Saving** goes through `setDay` (the day's hours as one manual entry with its half and typed text, a short code as
  an absence of that day alone), `setProjectDay` (one entry per project and day) and `setDayNote` (the note in the
  day's details). A day handed in refuses changes; a change to a day of an approved week where the organization
  approves weeks, of its hours, its projects or its note alike (`changeWay`), becomes a change request (`proposeDay`
  starts from what the open request already asks for the day, `requestChange` merges it), which the organization's
  page lists and `applyChange` books once approved (`changeFits` checks first that it asks only for the employer's
projects and the incomes of its own days). The details of a day (`POST /book/dayinfo`) keep its note. The
  note's field of a day whose employer is managed names the organization that sees it (`sheetEmployer.Org`,
  `data-note-org`, the field's `aria-describedby`); in the journal the line says it next to the note.
- **Preferences** (`POST /sheet/prefs`: `view`, `detail`, `expand`) are the person's `core.Preferences`. Without a view
  chosen the table opens; `Sec-CH-UA-Mobile: ?1` or, in the script, a screen narrower than 640 pixels opens the journal.
- **Layout.** `view.Sheet` makes the page not scroll itself: the sheet's own scroller does, so the head of the table,
  the column of days and the week's rows stick: the table is C22's sticky table (`table[data-variant=grid][data-sticky]`,
  the week a `tr[data-group-row]`, this week and today named by `aria-current`), and C22's `c22.js` keeps the week
  rows' offset at the height of the head. A field is C22's field in a cell (`td[data-field][data-type=number] > .input`,
  what is open, asked for or who sees the note in one `small` below it). The journal's line is C22's chip line
  (`.chip-input` with `[data-chips]`), the menu C22's popover without JavaScript (`details.popover` with
  `form[data-popover]`; `c22.js` closes it on Escape or a click outside and gives the focus back), and the employers
  carry C22's marks in turn (`.badge[data-variant=outline][data-mark=1…5]`, always with their key letter).

The week page (`/week`) keeps the tools of a mode (stopwatch, spans, plan, the week's total, projects only, free text,
absences of several days, handing in) and the correction of the time account (`POST /book/correction`,
`/corrections/{id}/remove`, for whoever may change the employer's settings, `core.MayChangeSettings`).

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
leave unbooked. The sheet shows that rest per day ("open"). `/report/week` renders a week to print (`view.Print`: no
header, tab bar or footer, the page inside C22's `print-sheet`), with a button that calls the browser's print.

## All employers, income, details of a day

`/all` (`all.go`) shows a week of every employer: a column per employer active in the week, a cell only on its
active days (hours, a code or an income), the earnings of each day (`core.ComputeEarnings` per employer and day, in
the currency of the first employer's pay), the week's sum, the average working day (a day of only mornings or only
afternoons counts half, `data-work-halves`) and what was taken in cash or by card; a cell with standby is marked.
The day page books incomes (`POST /book/income`, `/incomes/{id}/remove`) and the details of the day
(`POST /book/dayinfo`, with standby and its call-outs; all fields empty removes them) and says how much standby
counts as working time. In a week handed in or closed they stay as they are (the forms are gone or disabled); in an
approved week of an organization that approves weeks each of the three becomes a change request with the form's
`reason` (`changeDay`, through `changeWay` and `proposeDay` like the sheet), and the page shows what the open request
asks for the day (`data-day-request`, `data-day-pending`). The employer's settings take the amount for half a day of a rule per day, the standby bases
and whether standby is working time (`standby_work`).

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

