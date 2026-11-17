// SPDX-License-Identifier: MIT

package core

import "testing"

func TestOvertimeBasis(t *testing.T) {
	// Target 8 h Monday to Friday. Week 1: +2 h Monday, -2 h Tuesday. Week 2: +1 h Monday.
	entries := []Entry{
		work("2026-03-02", 600), work("2026-03-03", 360), work("2026-03-04", 480), work("2026-03-05", 480), work("2026-03-06", 480),
		work("2026-03-09", 540), work("2026-03-10", 480), work("2026-03-11", 480), work("2026-03-12", 480), work("2026-03-13", 480),
	}
	cases := []struct {
		basis    OvertimeBasis
		overtime int
	}{
		{BasisDay, 120 + 60}, // every surplus day counts, the short Tuesday does not offset it
		{BasisWeek, 0 + 60},  // week 1 nets to zero
		{"", 0 + 60},         // the default is the week
		{BasisMonth, 60},     // the month nets to +1 h
	}
	for _, c := range cases {
		s := week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)
		s.OvertimeBasis = c.basis
		data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeExact, "")}, Schedules: []Schedule{s},
			Adjustments: []Adjustment{
				{ID: "a1", Owner: owner, EmployerID: "e1", Date: d("2026-03-01"), Kind: AdjustOvertime, Minutes: 300, Reason: "carry-over"},
				{ID: "a2", Owner: owner, EmployerID: "e1", Date: d("2026-02-01"), Kind: AdjustOvertime, Minutes: 999, Reason: "outside"},
				{ID: "a3", Owner: owner, EmployerID: "e1", Date: d("2026-03-05"), Kind: AdjustLeave, Days: 2, Reason: "other kind"},
			}}, Entries: entries}
		acc, err := Overtime(data, "e1", d("2026-03-01"), d("2026-03-13"))
		if err != nil {
			t.Fatal(err)
		}
		if acc.Overtime != c.overtime || acc.Balance != 60+300 || acc.Adjustments != 300 {
			t.Errorf("basis %q: %+v, want overtime %d, balance 360", c.basis, acc, c.overtime)
		}
	}
}

func TestOvertimeIgnoresModesWithoutTarget(t *testing.T) {
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeWeekSum, "")},
		Schedules: []Schedule{week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)}},
		Entries: []Entry{work("2026-03-02", 2400)}}
	acc, err := Overtime(data, "e1", d("2026-03-01"), d("2026-03-31"))
	if err != nil || acc.Balance != 0 || acc.Overtime != 0 {
		t.Fatalf("week sum has no overtime: %+v %v", acc, err)
	}
}

func TestLeave(t *testing.T) {
	half := func(a Absence) Absence { a.HalfDay = true; return a }
	requested := func(a Absence) Absence { a.Status = StatusRequested; return a }
	fulltime := week("full", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)
	cases := []struct {
		name        string
		schedules   []Schedule
		absences    []Absence
		adjustments []Adjustment
		want        LeaveAccount
	}{
		{"a full year, a week off with a holiday in it", []Schedule{fulltime},
			// 2026-05-11 to 05-15 holds Ascension Day (Thursday 14th): four leave days.
			[]Absence{absence(AbsenceVacation, "2026-05-11", "2026-05-17")}, nil,
			LeaveAccount{Entitlement: 30, Taken: 4, Remaining: 26}},
		{"half days and a request", []Schedule{fulltime},
			[]Absence{half(absence(AbsenceVacation, "2026-06-01", "2026-06-01")), requested(absence(AbsenceVacation, "2026-07-06", "2026-07-07"))},
			nil, LeaveAccount{Entitlement: 30, Taken: 0.5, Requested: 2, Remaining: 29.5}},
		{"sickness does not use leave", []Schedule{fulltime},
			[]Absence{absence(AbsenceSick, "2026-06-01", "2026-06-05")}, nil, LeaveAccount{Entitlement: 30, Remaining: 30}},
		{"starting on 1 July", []Schedule{week("late", "2026-07-01", 480, 480, 480, 480, 480, 0, 0)}, nil, nil,
			// 184 of 365 days of 30 days is 15.1, rounded to the half day: 15.
			LeaveAccount{Entitlement: 15, Remaining: 15}},
		{"from full time to part time on 1 April", []Schedule{fulltime, func() Schedule {
			s := week("part", "2026-04-01", 480, 0, 480, 0, 0, 0, 0)
			s.LeaveDays = 12
			return s
		}()}, nil, nil,
			// 90 days at 30 plus 275 days at 12: 7.40 + 9.04 = 16.4, rounded to 16.5.
			LeaveAccount{Entitlement: 16.5, Remaining: 16.5}},
		{"part time: leave on a free day costs nothing", []Schedule{week("p", "2026-01-01", 480, 0, 480, 0, 0, 0, 0)},
			[]Absence{absence(AbsenceVacation, "2026-06-01", "2026-06-07")}, nil, LeaveAccount{Entitlement: 30, Taken: 2, Remaining: 28}},
		{"carry-over and a year boundary", []Schedule{fulltime},
			[]Absence{absence(AbsenceVacation, "2025-12-29", "2026-01-02")},
			[]Adjustment{{ID: "c", Owner: owner, EmployerID: "e1", Date: d("2026-01-01"), Kind: AdjustLeave, Days: 3, Reason: "carry-over"}},
			// Only 2026-01-02 is in the year: the 1st is New Year's Day.
			LeaveAccount{Entitlement: 30, Adjustments: 3, Taken: 1, Remaining: 32}},
		{"no schedule: Monday to Friday", nil, []Absence{absence(AbsenceVacation, "2026-06-01", "2026-06-07")}, nil,
			// Five weekdays, but Thursday 2026-06-04 is Corpus Christi in Bavaria.
			LeaveAccount{Taken: 4, Remaining: -4}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")}, Schedules: c.schedules,
				Adjustments: c.adjustments}, Absences: c.absences}
			got, err := Leave(data, "e1", 2026)
			if err != nil {
				t.Fatal(err)
			}
			c.want.Year = 2026
			if got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
