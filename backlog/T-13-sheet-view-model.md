---
id: T-13
type: Task
title: "The sheet: one view model, three views, the endless list from today"
status: erledigt
milestone: M-6
tags: [ui, keyboard]
created: 2026-11-19
---

- `web/sheet.go` builds the view model of a range of weeks: the employers with their keys, the columns of the detail,
  per week its days with a field per column (shown value, typed value, placeholder, hours not booked on projects,
  fixed, the value a change request asks for), the sums per column, total, target, difference, the running balance,
  the earnings and the state per employer (handed in, approved, change asked for), the gaps of the last eight weeks.
- `templates/sheet.html` renders it as a table, a journal or a grid; `app.js` holds one keyboard for all three and
  saves a field through `POST /sheet/cell`, whose answer is the weeks from the one changed on; only parts marked
  `data-slot` are taken over. Without JavaScript every week is a form (`POST /sheet/week`) that writes what changed.
- Older weeks: `GET /?part=weeks&before=…` (eight at a time), `?from=…` without JavaScript.

Done when unit tests prove the detail chosen automatically, open hours, balance, a public holiday and half days, and
browser tests prove ten keys per week in each view, Enter and Tab, the sticking week row, loading older weeks and the
change request in every view.
