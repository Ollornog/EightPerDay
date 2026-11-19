---
id: M-5
type: Milestone
title: "List view and leave planning: a start page that gives an overview"
status: erledigt
tags: [ui, simplicity, leave]
created: 2026-11-19
---

The first interface showed every tool of a mode at once, and people lost the overview. The start page becomes a plain
list of days and weeks, the way people keep their hours in a spreadsheet; everything else moves to tabs of its own.

**Done when this is true:**

- The start page is a list: this week and last week side by side (one above the other on a narrow screen). A day
  shows date, weekday, hours, target, difference and note; a week its sum, target, difference and the running balance
  (carried over from the week before). Below it the month and the year as a calendar grid with short codes.
- Weeks that are not finished show at the top as notices with a link to the week: working days without an entry,
  a plan week not confirmed, a week not handed in where the organization approves weeks.
- A second list covers the whole period: the newest week at the bottom, older weeks load while scrolling up, without
  pages.
- Older and confirmed weeks can be changed. On a private employer the change is simply made and stays visible in the
  history. On a managed employer a change to an approved week becomes a change request through the approval (T-1),
  which the organization decides; until then the approved values count and the request is marked. Its decision goes
  into the access log like every other one.
- Browser tests prove the side-by-side weeks, the notices, the endless list and both ways of changing an older week.
- Hours are typed as a number into the day's row (`8,5`); start, end and break stay optional. Short codes instead of
  hours: `U`/`V` leave, `K`/`S` sick, `F`/`O` day off, half days (`½U`); public holidays come from the region; leave
  and sickness meet the target.
- A week of five days is entered with the keyboard alone in at most ten key presses (a browser test counts them).
  A half day, the morning or the afternoon, is one letter behind the hours (`4v`, `4n`), and half a day of leave
  stands next to hours of the same day (`½U 4n`); a browser test counts these keys as well.
- A correction of the time account (payout, cap) is a booking of its own with a reason, and the balance shows it.
- Bookings on projects or clients per day (name, order number, hours), both ways: the projects make the day's hours,
  or the day's hours come first and the list shows what is not booked on projects, per day and week.
- A weekly report to print: carry-over, difference, balance, and per day the clients with numbers and hours.
- Several employers in one list, each with its own pay: per hour, per day or half day, per job with a client and an
  amount, plus extras (commission, photos, tips), taken in cash or by card; earnings per day, week and the average day.
  Pay per day knows an own amount for half a day (morning or afternoon, otherwise half), and the average counts half
  days as halves. Optionally per employer: place of work and kilometres per day, standby with its pay (passive and
  active, per hour, per standby day or per call-out) in the earnings, and whether standby is working time (active by
  default). One currency per instance and employer.
- Leave: entitlement, accrued up to today, taken, planned, remaining, half days. Planned days are marked in the year's
  grid and become a request (T-1) for approval; in an organization a reviewer sees who else is away at the same time.
- An optional estimate of pay: a yearly or monthly salary or an hourly rate, with 13th and 14th month pay.

Tasks: [T-6](T-6-list-and-codes.md), [T-9](T-9-endless-list-and-change-requests.md),
[T-7](T-7-projects-report-employers.md), [T-8](T-8-leave-planning.md), [T-10](T-10-employers-in-one-list.md),
[T-12](T-12-half-days-and-standby-pay.md).
