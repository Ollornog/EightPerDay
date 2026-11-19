---
id: T-7
type: Task
title: "Projects per day and the printable weekly report"
status: erledigt
milestone: M-5
tags: [ui, core, projects]
created: 2026-11-19
---

- Bookings per day on projects or clients with an order number (`Project.Number`) and hours, on a page of the day
  (`/day`), linked from the list.
- Both ways (`core.DayTotal`): hours typed for a day hold its bookings on projects, and the list shows what is not
  booked on projects per day and week (`Day.Unbooked`, `Total.Unbooked`); without typed hours the bookings make the
  day. A free-text line still adds to them.
- A printable weekly report (`/report/week`): per day the clients with numbers and hours, the sum, target,
  difference, carry-over, corrections and balance; the browser prints it or saves it as a PDF.

The rest of the first plan of this task (several employers in one list, pay per job with extras, kilometres,
standby, an estimate of pay) moved to [T-10](T-10-employers-in-one-list.md).
