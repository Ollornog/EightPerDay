// SPDX-License-Identifier: MIT

package core

import (
	"math"
	"slices"
	"time"
)

// OvertimeAccount is the time account of an employer over a range.
type OvertimeAccount struct {
	// Balance is the sum of the daily balances plus the overtime adjustments in the range.
	Balance int `json:"balance"`
	// Overtime sums only the surplus of each period of the overtime basis (day, week or month of the schedule, week if
	// it names none): a day or week with less time does not eat into the surplus of another.
	Overtime int `json:"overtime"`
	// Adjustments is the part of Balance that comes from adjustments (carry-over, payout, correction).
	Adjustments int `json:"adjustments"`
}

// Overtime calculates the time account of the employer from from to to.
func Overtime(d Data, employerID string, from, to Date) (OvertimeAccount, error) {
	days, err := Days(d, employerID, from, to)
	if err != nil {
		return OvertimeAccount{}, err
	}
	emp, _ := d.Catalog.Employer(employerID)
	var acc OvertimeAccount
	for _, day := range days {
		acc.Balance += boolInt(day.HasTarget) * day.Balance
	}
	for _, p := range overtimePeriods(days, d.Catalog, emp) {
		acc.Overtime += p.surplus
	}
	for _, a := range d.Catalog.Adjustments {
		if a.EmployerID == employerID && a.Kind == AdjustOvertime && a.Date.Within(from, to) {
			acc.Adjustments += a.Minutes
		}
	}
	acc.Balance += acc.Adjustments
	return acc, nil
}

// overtimePeriod is one period of the overtime basis with its surplus (zero if it had none).
type overtimePeriod struct {
	from, to Date
	surplus  int
}

// overtimePeriods groups the days by the overtime basis valid at the start of each group (a schedule change starts a
// new group) and returns the surplus of each: per day for the basis day, else the positive sum of the group.
func overtimePeriods(days []Day, cat Catalog, emp Employer) []overtimePeriod {
	var out []overtimePeriod
	var group []Day
	var basis OvertimeBasis
	var key Date
	flush := func() {
		if len(group) == 0 {
			return
		}
		if basis == BasisDay {
			for _, g := range group {
				out = append(out, overtimePeriod{g.Date, g.Date, boolInt(g.HasTarget && g.Balance > 0) * g.Balance})
			}
		} else {
			t := Sum(group)
			out = append(out, overtimePeriod{t.From, t.To, max(0, t.Balance)})
		}
		group = group[:0]
	}
	for _, day := range days {
		s, _ := ScheduleAt(cat, emp.ID, day.Date)
		b := s.OvertimeBasis
		var k Date
		switch b {
		case BasisWeek, "":
			k = periodStart(day.Date, PeriodWeek, emp.WeekStart)
		case BasisMonth:
			k = periodStart(day.Date, PeriodMonth, emp.WeekStart)
		default:
			k = day.Date
		}
		if len(group) > 0 && (k != key || b != basis) {
			flush()
		}
		basis, key = b, k
		group = append(group, day)
	}
	flush()
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// LeaveAccount is the leave of one employer in one calendar year, in days.
type LeaveAccount struct {
	Year int `json:"year"`
	// Entitlement is the leave of the year, prorated by the days each schedule is valid, rounded to half days.
	Entitlement float64 `json:"entitlement"`
	// Adjustments are carry-overs and corrections dated in the year.
	Adjustments float64 `json:"adjustments"`
	// Taken counts approved leave on working days (target > 0) that are not full public holidays.
	Taken float64 `json:"taken"`
	// Requested counts leave that waits for approval.
	Requested float64 `json:"requested"`
	// Remaining is Entitlement + Adjustments - Taken.
	Remaining float64 `json:"remaining"`
}

// Leave calculates the leave account of the employer for the year. The data must hold the absences of the year.
// Without a schedule, Monday to Friday count as working days.
func Leave(d Data, employerID string, year int) (LeaveAccount, error) {
	v, err := newEmployerView(d, employerID)
	if err != nil {
		return LeaveAccount{}, err
	}
	first, last := NewDate(year, time.January, 1), NewDate(year, time.December, 31)
	acc := LeaveAccount{Year: year}

	// Entitlement: each schedule for the days of the year it is valid.
	daysInYear := float64(first.DaysUntil(last) + 1)
	entitled := 0.0
	for i, s := range v.schedules {
		start := s.ValidFrom
		end := last
		if i+1 < len(v.schedules) {
			end = v.schedules[i+1].ValidFrom.AddDays(-1)
		}
		if start.Before(first) {
			start = first
		}
		if end.After(last) {
			end = last
		}
		if end.Before(start) {
			continue
		}
		entitled += s.LeaveDays * float64(start.DaysUntil(end)+1) / daysInYear
	}
	acc.Entitlement = math.Round(entitled*2) / 2

	for _, a := range d.Catalog.Adjustments {
		if a.EmployerID == employerID && a.Kind == AdjustLeave && a.Date.Year == year {
			acc.Adjustments += a.Days
		}
	}

	types := d.Catalog.AbsenceTypesFor(employerID)
	for _, a := range d.Absences {
		if a.DeletedAt != nil || !appliesTo(v.emp, a.EmployerID) {
			continue
		}
		t, ok := findAbsenceType(types, a.Type)
		if !ok || !t.DeductsLeave || a.Status == StatusRejected {
			continue
		}
		n := 0.0
		for day := a.From; !day.After(a.To); day = day.AddDays(1) {
			if day.Year != year || !v.workingDay(day) {
				continue
			}
			if _, frac := v.holidays.on(day); frac >= 1000 {
				continue
			}
			if a.HalfDay {
				n += 0.5
			} else {
				n++
			}
		}
		if a.Status == StatusApproved {
			acc.Taken += n
		} else {
			acc.Requested += n
		}
	}
	acc.Remaining = acc.Entitlement + acc.Adjustments - acc.Taken
	return acc, nil
}

// workingDay reports whether the schedule has a target on the day; without one, Monday to Friday.
func (v *employerView) workingDay(d Date) bool {
	if s, ok := v.scheduleAt(d); ok {
		return s.Days[d.Weekday()].Minutes > 0
	}
	wd := d.Weekday()
	return wd != time.Saturday && wd != time.Sunday
}

// LeaveStatus is the leave account of a year as of a day: what is earned up to it, taken up to it and planned after it.
type LeaveStatus struct {
	LeaveAccount
	// Today is the day the status is taken on.
	Today Date `json:"today"`
	// Accrued is the part of the entitlement earned up to today (aliquot): each schedule's leave for the days of the year
	// it is valid up to today, rounded to half days. The whole entitlement once the year is over.
	Accrued float64 `json:"accrued"`
	// TakenSoFar counts approved leave up to today, Planned approved leave after it; together they are Taken.
	TakenSoFar float64 `json:"taken_so_far"`
	Planned    float64 `json:"planned"`
}

// LeaveAt is Leave as of today: the account of the year with the accrued part and taken leave split into what lies up
// to today and what is planned after it. Half days count half; public holidays and days without a target count not.
func LeaveAt(d Data, employerID string, year int, today Date) (LeaveStatus, error) {
	acc, err := Leave(d, employerID, year)
	if err != nil {
		return LeaveStatus{}, err
	}
	v, err := newEmployerView(d, employerID)
	if err != nil {
		return LeaveStatus{}, err
	}
	st := LeaveStatus{LeaveAccount: acc, Today: today}
	first, last := NewDate(year, time.January, 1), NewDate(year, time.December, 31)
	until := today
	if until.After(last) {
		until = last
	}
	daysInYear := float64(first.DaysUntil(last) + 1)
	accrued := 0.0
	for i, s := range v.schedules {
		start, end := s.ValidFrom, last
		if i+1 < len(v.schedules) {
			end = v.schedules[i+1].ValidFrom.AddDays(-1)
		}
		start, end = maxDate(start, first), minDate(end, until)
		if end.Before(start) {
			continue
		}
		accrued += s.LeaveDays * float64(start.DaysUntil(end)+1) / daysInYear
	}
	st.Accrued = math.Round(accrued*2) / 2
	if today.After(last) {
		st.Accrued = acc.Entitlement
	}
	types := d.Catalog.AbsenceTypesFor(employerID)
	for _, a := range d.Absences {
		if a.DeletedAt != nil || !appliesTo(v.emp, a.EmployerID) || a.Status != StatusApproved {
			continue
		}
		if t, ok := findAbsenceType(types, a.Type); !ok || !t.DeductsLeave {
			continue
		}
		for day := a.From; !day.After(a.To); day = day.AddDays(1) {
			if day.Year != year || !v.workingDay(day) {
				continue
			}
			if _, frac := v.holidays.on(day); frac >= 1000 {
				continue
			}
			n := 1.0
			if a.HalfDay {
				n = 0.5
			}
			if day.After(today) {
				st.Planned += n
			} else {
				st.TakenSoFar += n
			}
		}
	}
	return st, nil
}

// Overlaps returns the leave of other people that falls on days of the absence, for a reviewer who decides a request:
// only types that count against leave (never sickness), approved or requested, ordered by start.
func Overlaps(d Data, a Absence) []Absence {
	var out []Absence
	for _, x := range d.Absences {
		if x.DeletedAt != nil || x.Owner == a.Owner || x.Status == StatusRejected || x.To.Before(a.From) || x.From.After(a.To) {
			continue
		}
		t, ok := findAbsenceType(d.Catalog.AbsenceTypesFor(x.EmployerID), x.Type)
		if !ok || !t.DeductsLeave || t.Health {
			continue
		}
		out = append(out, x)
	}
	slices.SortStableFunc(out, func(a, b Absence) int { return a.From.Compare(b.From) })
	return out
}
