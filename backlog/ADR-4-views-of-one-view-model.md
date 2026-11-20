---
id: ADR-4
type: Decision
title: "Three views as renderers of one view model, preferences per person"
status: erledigt
tags: [architecture, ui]
created: 2026-11-19
---

## Context

The maintainer liked three mockups of the start page (a table, a journal, a grid per week) and wants all three,
switchable, with a level of detail that follows the records. Three pages with their own numbers would drift apart: the
balance of one would differ from another, and every fix would be needed three times.

## Options

- **Three pages, each with its own handler and calculation.**
- **One view model in Go and three templates that only render it**, one keyboard model in JavaScript for all three.
- **A client application** that computes in the browser from the records.

## Decision

The second option.

- `web/sheet.go` is the view model: weeks, days, columns per detail, sums, balance, earnings, open hours, the state of
  each week. All numbers come from the core (`Days`, `Ledger`, `WeekGaps`, `ComputeEarnings`); the view model only
  arranges them. The three templates in `templates/sheet.html` read the same struct; the columns of the table are the
  rows of the grid, and the journal's line is written from the same fields.
- A saved field is answered with the weeks rendered by the server; the page takes over only the parts marked
  `data-slot`, so a field being typed into is never replaced and nothing is computed twice.
- Detail: automatic picks hours (one employer without projects), projects (one employer with projects) or employers
  (several). Hours and projects show the first employer with a schedule; the other employers are summed into the day
  and named ("+3 other"), never dropped. A field of the employer's hours next to projects takes the day's hours, and
  what the projects leave is "open" (`core.DayTotal`, `Day.Unbooked`).
- A day belongs to the first employer with a target that day (or the first with a schedule): hours without a key in
  the journal and the note of the day go to it.
- Preferences are a catalog record of the person (`core.Preferences`), not of the browser and not of the sign-in
  service, so they work the same in the standalone program and inside an embedding application.
- The layout has no stylesheet of its own: C22's table (`data-variant="grid"`), inputs, badges and utilities; the offset
  of the week's row below the head of the table is set by the script, which C22 cannot do yet.

## Consequences

- A new view is a template, not a calculation. A number that is wrong is wrong in all three, and fixed in one place.
- The journal's line and the fields share `core.ParseDay`; the list's own pattern and parser are gone.
- A span typed into a field keeps its text (`Entry.Typed`) next to the hours; start and end of such a day are not
  stored as a span, and the hints on rest periods read them back from the text (`core.TypedSpans`, since T-17).
- A note of a day lives in the details of the day (`DayInfo.Note`) of the day's employer; for a managed employer the
  organization sees it, like every record of that employer. Decided by the maintainer on 2026-11-20 (T-17): it stays
  so, and the field says it in every view and in the form without JavaScript ("visible to <organization>", German
  "sichtbar für <Organisation>"); a private employer's note says nothing of the kind. What is private belongs to a
  private employer's day.
