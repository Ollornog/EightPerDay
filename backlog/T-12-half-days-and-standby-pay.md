---
id: T-12
type: Task
title: "Pay for half days, paid standby, one currency"
status: erledigt
milestone: M-5
tags: [ui, core, pay]
created: 2026-11-19
---

Seasonal work is paid per day with a morning or an afternoon (ski lessons), and some employers pay standby. Decided by
the maintainer on 2026-11-19: half days and paid standby yes, several currencies no.

- **Half days:** a pay rule per day (`per_assignment`, unit day) knows an own amount for half a day
  (`PayRule.HalfAmount`); without one half the amount counts. Work marks its half (`Entry.Part`: morning or
  afternoon); a day whose work took only one half (`Day.Part`) pays half a day. The list reads a letter behind the
  hours (`4v`/`4a` morning, `4n`/`4p` afternoon, also `4 VM`, `4 NM`, `4 AM`, `4 PM`): one key more than a whole day,
  so a week of whole days stays at ten key presses. The earnings per day and week follow; the average of the week of
  every employer counts a day of only mornings or only afternoons as half a day.
- **Half a day of leave with hours:** the list takes half a day of a code next to hours (`½U 4n`), so the open point
  "half a day of leave together with hours" is solved here: the leave covers half the target, the hours count, and a
  half day of work is paid as one. Change requests carry the same (`ProposedDay.Part`, minutes next to a half
  absence).
- **Standby:** pay rules of the bases `standby_passive` and `standby_active`, per hour, per day with standby, or per
  call-out (active only, `DayInfo.Callouts`); the model is in [ADR-3](ADR-3-half-days-and-standby.md). Standby pay is
  in the earnings of the day and the week. `Employer.StandbyWork` decides what counts as working time for the time
  account: active standby by default, none, or all (labour law usually counts it so; this is no legal advice).
- **Several currencies: rejected.** One currency per instance and employer, as before. Nothing is built for it; a
  person with employers in two currencies sees each employer's pay in its currency, and the week of every employer
  sums the currency of the first employer.

Done: core (`Day.Part`, `Day.Standby`, `ComputeEarnings` with half days and standby lines), list, day page (call-outs,
standby as working time), `/all` (halves in the average, standby marked), employer settings (amount for half a day,
standby bases, standby as working time), report labels; tests in `core` and `web`, browser tests
`TestHalfDaysWithTheKeyboard` and `TestStandbyOnAPhone`.
