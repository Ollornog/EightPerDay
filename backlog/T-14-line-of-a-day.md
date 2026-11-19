---
id: T-14
type: Task
title: "The line of a day: the core's parser for one day, employer keys, jobs by a word"
status: erledigt
milestone: M-6
tags: [core, parser]
created: 2026-11-19
---

- `core.ParseDay` reads a day the way Parse reads a line (durations, spans with their break, `#tags`, `@project`, the
  rest a note) with the short codes of a day, the half of a day behind hours (4v, 4 NM), plain numbers as hours, the
  key of an employer in front (f3) and a word naming a job behind (f3 studio, by name or order number,
  `core.MatchProject`). Every field of the sheet reads its value with it; the journal reads the whole line.
- `Employer.Key` (one to three letters), `core.EmployerKeys` for employers without one; `core.ProjectWord` writes the
  job back so it reads the same. `Entry.Typed` keeps a span with its break as typed; `DayInfo.Note` holds the note of a
  day with or without hours.

Done when the unit tests cover the syntax of the mockup and its refusals.
