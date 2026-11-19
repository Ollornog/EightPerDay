---
id: T-16
type: Task
title: "Clean-up: the former list and /weeks go, the week page keeps its tools"
status: erledigt
milestone: M-6
tags: [ui, cleanup]
created: 2026-11-19
---

- Gone: the list of one employer (`list.html`, `POST /book/list`, the month and year grid of the start page, the
  pattern of a row), `/weeks` and the form with the hours per day of the mode schedule on `/week` (`POST /book/days`).
- The week page keeps the tools of a mode (stopwatch, spans, the plan, the week's total, projects only, free text,
  absences of several days, handing in) and takes the correction of the time account. The weekly report, the week of
  every employer (`/all`), the day page and leave planning are linked from the sheet.
