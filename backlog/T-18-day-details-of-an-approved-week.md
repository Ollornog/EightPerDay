---
id: T-18
type: Task
title: "The details and incomes of a day of an approved week as change requests, locked in the core"
status: erledigt
milestone: M-6
tags: [approval, standby, income]
created: 2026-11-20
---

Decision 2026-11-20: standby, call-outs and incomes change the pay, so an approved week is fixed as a
whole; the other details of a day go the way of hours, projects and note (T-17).

- The core locks the details of a day (`DayInfo`: place, kilometres, standby, call-outs, note) and its incomes
  (`Income`, cash, card, transfer) with the day: both stores refuse to write, move or delete them on a day that is
  closed, handed in or approved (`Catalog.DayRecordLocked`, `ErrLocked`), tested in `core/storetest`. An embedding
  application gets the lock without doing anything.
- A change request carries them: `ProposedDay.Details` (`ProposedDetails`, all of the details but the note) and
  `ProposedDay.Incomes` (`ProposedIncome`, an income to add or, with `Remove`, one to take off), validated like the
  records themselves.
- The day's page: in an approved week of an organization that approves weeks, saving the details, adding an income
  and taking one off become a change request with a reason, and the page shows what is asked for; in a week handed
  in or closed the forms are gone or disabled. The note keeps its ways (sheet, journal, form without JavaScript).
- The reviewer reads the details and incomes asked for; approving books them, after checking that the request takes
  off only incomes of its own days; the access log shows it.

Done when the conformance test (`details and incomes of a locked day`, both stores), the core's `TestChangeRequest`,
the web tests `TestDayDetailsAndIncomesOfAnApprovedWeek`, `TestChangeRequestTakesOffOnlyItsDaysIncomes`,
`TestSheetChangeRequestForNotesAndProjects`, `TestEmbeddedBooking` and the browser test `TestChangeRequestInEveryView`
pass, and each rule is shown by a mutation.
