---
id: M-6
type: Milestone
title: "Views and detail: one sheet in three views, typed in fewer keys"
status: erledigt
blocked_by: [M-5]
tags: [ui, simplicity, keyboard]
created: 2026-11-19
---

The list of M-5 still showed one employer at a time and needed a click per week to save; people who keep their hours
in a spreadsheet see more at once there: several employers side by side, and an employer with its jobs side by side
per day. The start page becomes one endless sheet that opens at today, in three views of the same numbers, with as much
detail as the records need.

**Done when this is true:**

- The start page is one endless list of weeks for every employer of the person: it opens at this week (marked), older
  weeks load while scrolling up (without JavaScript a link opens the page further up), two weeks to plan follow.
  "This week" and the list of every week are one page.
- Three views of it, one view model ([ADR-4](ADR-4-views-of-one-view-model.md)): a table (days as rows, employers and
  their jobs as columns, the week's row with its sums, balance and earnings sticks on top, the head and the dates
  stick too), a journal (one line per day, typed in one field, shown as chips without the focus) and a grid per week
  (employers and jobs as rows, days as columns, a row of sums, of differences and of notes).
- The detail: automatic (one employer without projects: hours only; one with projects: the projects as columns; several
  employers: a column each, their projects to open), or chosen: hours only, projects, employers. A detail chosen by hand
  holds when the records say more; what it hides is summed into the day, never lost.
- View and detail are kept per person; without a choice the table, on a narrow screen the journal.
- The keyboard works like a spreadsheet in every view: typing replaces, Enter or ↓ to the next day (the next row in the
  grid), Shift+Enter or ↑ back, Tab and the arrows at the edge to the next field, Esc takes back. A field shows the
  hours and gives back what was typed (8-13:30 30m shows 5). Each field saves itself without a page change and shows
  an error at the field.
- A week of five days takes at most ten keys in each view (browser tests count them).
- The journal reads one line per day with the core's parser: 5 for the day's own employer, f3 for the employer with
  the key f, f3 studio for its job, n1,5, the codes U K F ½U 2,5n, the rest a note.
- A change to an approved week of a managed employer becomes a change request in all three views (T-9, T-1).
- The former list, `/weeks` and the day form of the week page are gone; report, approval, leave planning and the
  correction of the time account stay reachable.

Tasks: [T-13](T-13-sheet-view-model.md), [T-14](T-14-line-of-a-day.md), [T-15](T-15-preferences.md),
[T-16](T-16-clean-up-list-and-week.md); follow-ups in [T-17](T-17-m6-follow-ups.md) (notes and projects of an approved
week as change requests, typed spans for the rest) and [T-18](T-18-day-details-of-an-approved-week.md) (the details and
incomes of an approved day as change requests, locked in the core). The first release ([M-4](M-4-first-release.md)) follows after the
maintainer has accepted it.
