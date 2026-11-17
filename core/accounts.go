// SPDX-License-Identifier: MIT

package core

import (
	"math"
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
