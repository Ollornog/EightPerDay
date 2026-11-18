// SPDX-License-Identifier: MIT

package core

import "slices"

// LedgerWeek is one week of a list of days, the way people keep their hours in a spreadsheet: the days, their sums,
// the corrections of the time account dated in the week and the balance carried from week to week.
type LedgerWeek struct {
	From Date  `json:"from"`
	To   Date  `json:"to"`
	Days []Day `json:"days"`
	// Total sums the days up to the ledger's until day; a day after it is listed but counts for nothing yet, since a
	// day that has not come is short of nothing.
	Total Total `json:"total"`
	// CarryOver is the balance of the time account at the end of the week before.
	CarryOver int `json:"carry_over"`
	// Corrections are the overtime adjustments (payout, cap, carry-over) dated in the week, up to the until day.
	Corrections []Adjustment `json:"corrections,omitempty"`
	// Balance is the account at the end of the week: CarryOver, the balance of the counted days from the account's
	// start on, and the corrections.
	Balance int `json:"balance"`
}

// Ledger lists the weeks of an employer from the one holding from to the one holding to, with the running balance of
// its time account since start (usually the first schedule's valid-from day). Days after until count for nothing.
// The balance of the last week up to until equals Overtime(d, employerID, start, until).Balance. The data must hold
// the records from start (or from, if earlier) to the end of to's week.
func Ledger(d Data, employerID string, start, from, to, until Date) ([]LedgerWeek, error) {
	emp, ok := d.Catalog.Employer(employerID)
	if !ok {
		return nil, invalid("unknown employer")
	}
	first := from.StartOfWeek(emp.WeekStart)
	last := to.StartOfWeek(emp.WeekStart).AddDays(6)
	if last.Before(first) {
		return nil, invalid("range ends before it starts")
	}
	begin := minDate(first, start)
	days, err := Days(d, employerID, begin, last)
	if err != nil {
		return nil, err
	}
	var corrections []Adjustment
	for _, a := range d.Catalog.Adjustments {
		if a.EmployerID == employerID && a.Kind == AdjustOvertime && !a.Date.Before(start) && !a.Date.After(until) {
			corrections = append(corrections, a)
		}
	}
	slices.SortStableFunc(corrections, func(a, b Adjustment) int { return a.Date.Compare(b.Date) })

	var out []LedgerWeek
	balance := 0
	var week *LedgerWeek
	for _, day := range days {
		if !day.Date.Before(first) && (week == nil || day.Date.After(week.To)) {
			out = append(out, LedgerWeek{From: day.Date, To: day.Date.AddDays(6), CarryOver: balance})
			week = &out[len(out)-1]
		}
		counted := !day.Date.After(until)
		if counted && day.HasTarget && !day.Date.Before(start) {
			balance += day.Balance
		}
		for _, c := range corrections {
			if c.Date == day.Date {
				balance += c.Minutes
				if week != nil {
					week.Corrections = append(week.Corrections, c)
				}
			}
		}
		if week == nil {
			continue
		}
		week.Days = append(week.Days, day)
		if counted {
			week.Total.add(day)
		}
		week.Balance = balance
	}
	return out, nil
}

func minDate(a, b Date) Date {
	if b.Before(a) {
		return b
	}
	return a
}

// WeekGap is what a week still lacks, for a notice that leads the person to it.
type WeekGap struct {
	From Date `json:"from"`
	To   Date `json:"to"`
	// Missing are the working days before today with a target left after holidays and absences, and nothing booked:
	// no work, no absence (a plan day of the mode plan counts as worked).
	Missing []Date `json:"missing,omitempty"`
	// Unconfirmed: the week is over and a plan day of it is not confirmed (mode plan).
	Unconfirmed bool `json:"unconfirmed,omitempty"`
	// NotSubmitted: the week is over, the organization approves weeks, and a working day of it is neither handed in nor
	// approved.
	NotSubmitted bool `json:"not_submitted,omitempty"`
}

// WeekGaps lists the weeks from the one holding from up to the one holding today that lack something, oldest first.
// approval says whether the employer's organization approves weeks. Days before start (the account's start) lack
// nothing. Modes without a target lack nothing either.
func WeekGaps(d Data, employerID string, start, from, today Date, approval bool) ([]WeekGap, error) {
	emp, ok := d.Catalog.Employer(employerID)
	if !ok {
		return nil, invalid("unknown employer")
	}
	first := maxDate(from, start).StartOfWeek(emp.WeekStart)
	last := today.StartOfWeek(emp.WeekStart).AddDays(6)
	if last.Before(first) {
		return nil, nil
	}
	days, err := Days(d, employerID, first, last)
	if err != nil {
		return nil, err
	}
	var out []WeekGap
	for i := 0; i < len(days); i += 7 {
		g := WeekGap{From: days[i].Date, To: days[i].Date.AddDays(6)}
		over := g.To.Before(today)
		for _, day := range days[i:min(i+7, len(days))] {
			if !day.HasTarget || day.Date.Before(start) || day.Target == 0 {
				continue
			}
			past := day.Date.Before(today)
			if past && day.AdjustedTarget > 0 && day.Worked == 0 && day.Credited == 0 && len(day.Absences) == 0 {
				g.Missing = append(g.Missing, day.Date)
			}
			if over && day.FromPlan && !day.Confirmed {
				g.Unconfirmed = true
			}
			if over && approval && day.AdjustedTarget > 0 && !d.Catalog.SubmittedLocked(employerID, day.Date) {
				g.NotSubmitted = true
			}
		}
		if len(g.Missing) > 0 || g.Unconfirmed || g.NotSubmitted {
			out = append(out, g)
		}
	}
	return out, nil
}

func maxDate(a, b Date) Date {
	if b.After(a) {
		return b
	}
	return a
}
