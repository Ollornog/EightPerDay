---
id: T-17
type: Task
title: "M-6 follow-ups: notes and projects of an approved week as change requests, typed spans for the rest"
status: erledigt
milestone: M-6
tags: [approval, arbzg, sheet]
created: 2026-11-20
---

- A change to the note or to the hours on a project of an approved week becomes a change request, like a change of
  the hours: in the table's field (`POST /sheet/cell`), in the week's form without JavaScript (`POST /sheet/week`) and
  in the journal's line (`core.ParseDay`). One request holds the hours, projects and note of a day; the reviewer reads
  them all; approving books them and the access log shows it. `ProposedDay` carries `Projects` and `Typed`.
- Saving the details of a day keeps its note.
- The note of a managed employer's day stays visible to its organization (maintainer, 2026-11-20); its field says so
  in every view and in the form without JavaScript ("visible to <organization>"), a private employer's does not.
- Spans typed for the hours of a day count for the rest between two working days (§ 5 ArbZG) in the person's
  overview (`core.TypedSpans`).

Done when the tests of the web package (`TestSheetChangeRequestForNotesAndProjects`,
`TestSheetHandedInWeekKeepsProjects`, `TestRestHintFromTypedSpans`, `TestSheetNoteVisibleToTheOrganization`), of the core (`TestRestHintsFromTypedSpans`,
`TestChangeRequest`) and the browser test `TestChangeRequestInEveryView` pass, and each fix is shown by a mutation.
