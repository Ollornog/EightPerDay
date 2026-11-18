---
id: T-6
type: Task
title: "The list as the start page: hours per row, short codes, running balance, corrections"
status: erledigt
milestone: M-5
tags: [ui, simplicity, core]
created: 2026-11-19
---

- `core.ParseDayCode` reads the short codes of a day (`U`, `V`, `K`, `S`, `F`, `O`, half days with `½`, `/2` or
  `0,5`) as an absence type; `core.Ledger` gives the weeks of a list with their days, sums up to a day, the
  corrections dated in them and the running balance carried over from the account's start.
- `/` is the list of the chosen employer: this week and last week side by side, unfinished weeks as notices on top
  with a link to the week, the month and the year as a grid of codes below. `?w=` shows any older week the same way;
  on a private employer it is changed like this week, the history keeps every change. Every row takes hours or a
  code; Enter saves and the next empty working day gets the focus. The former week view with its tools (stopwatch, free text, plan, approval) moves to the tab `/week`.
- A correction of the time account with a reason (`POST /book/correction`) for whoever may change the employer's
  settings; the week it falls in shows it.

Done when the browser test enters a week of five days in at most ten key presses, and the unit tests prove codes,
half days, running balance and corrections.
