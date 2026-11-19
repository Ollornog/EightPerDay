// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"testing"
)

func standbyDay(date string, passive, active, callouts int) DayInfo {
	return DayInfo{ID: DayInfoID("e1", d(date)), Owner: owner, EmployerID: "e1", Date: d(date), StandbyPassive: passive,
		StandbyActive: active, Callouts: callouts}
}

// Which standby counts as working time: active only by default, none, or all; it fills the day's hours and the
// balance, in every mode, and the plan of a plan day stays the plan.
func TestStandbyAsWorkingTime(t *testing.T) {
	infos := []DayInfo{standbyDay("2026-01-05", 480, 90, 1)}
	for _, c := range []struct {
		work StandbyWork
		mode Mode
		want int
	}{
		{"", ModeSchedule, 90},
		{StandbyWorkActive, ModeSchedule, 90},
		{StandbyWorkNone, ModeSchedule, 0},
		{StandbyWorkAll, ModeSchedule, 570},
		{"", ModeProjectsOnly, 90},
		{"", ModePlan, 90},
	} {
		emp := employerOf(c.mode, "")
		emp.StandbyWork = c.work
		if err := emp.Validate(); err != nil {
			t.Fatal(err)
		}
		data := Data{Catalog: Catalog{Employers: []Employer{emp}, DayInfos: infos,
			Schedules: []Schedule{week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)}}}
		day := dayOf(t, mustDays(t, data, "2026-01-05", "2026-01-05"), "2026-01-05")
		base := 0
		if c.mode == ModePlan {
			base = 480
		}
		if day.Standby != c.want || day.Worked != base+c.want {
			t.Errorf("%q in %s: standby %d, worked %d, want %d", c.work, c.mode, day.Standby, day.Worked, base+c.want)
		}
		if c.mode == ModePlan {
			entries, err := MaterializePlan(data, "e1", d("2026-01-05"), d("2026-01-05"))
			if err != nil || len(entries) != 1 || entries[0].Minutes != 480 {
				t.Errorf("the plan turned into %+v (%v)", entries, err)
			}
		}
	}
	bad := employerOf(ModeSchedule, "")
	bad.StandbyWork = "sometimes"
	if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown standby setting: %v", err)
	}
}

// Standby pay: passive per hour or per standby day, active per hour or per call-out; it adds to the week, and standby
// counted as work is not paid again at the hourly rule.
func TestStandbyPay(t *testing.T) {
	standby := func(id string, basis PayBasis, amount int64, unit string) PayRule {
		r := rule(id, basis, amount, "2026-01-01", "")
		r.Unit = unit
		return r
	}
	infos := []DayInfo{standbyDay("2026-01-05", 600, 120, 2), standbyDay("2026-01-06", 480, 0, 0),
		standbyDay("2026-01-07", 0, 60, 0)}
	hourly := rule("h", PayHourly, 2000, "2026-01-01", "")
	cases := []struct {
		name  string
		rules []PayRule
		want  int64
	}{
		{"no standby rule", nil, 0},
		{"passive per hour", []PayRule{standby("p", PayStandbyPassive, 300, "")}, 300 * (10 + 8)},
		{"passive per day", []PayRule{standby("p", PayStandbyPassive, 2500, UnitDay)}, 2500 * 2},
		{"active per hour", []PayRule{standby("a", PayStandbyActive, 2400, "")}, 2400 * 3},
		{"active per day", []PayRule{standby("a", PayStandbyActive, 4000, UnitDay)}, 4000 * 2},
		// Two call-outs on Monday, one on Wednesday (minutes without a count are one call-out).
		{"active per call-out", []PayRule{standby("a", PayStandbyActive, 5000, UnitAssignment)}, 5000 * 3},
		{"both", []PayRule{standby("p", PayStandbyPassive, 300, ""), standby("a", PayStandbyActive, 5000, UnitAssignment)},
			300*18 + 5000*3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, r := range c.rules {
				if err := r.Validate(); err != nil {
					t.Fatal(err)
				}
			}
			data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeExact, "")}, DayInfos: infos,
				PayRules: append([]PayRule{hourly}, c.rules...)}, Entries: []Entry{work("2026-01-05", 60)}}
			e := earnings(t, data, "2026-01-05", "2026-01-11")
			// One hour of work at 20,00; the standby is paid by its rules alone.
			if e.Total["EUR"] != 2000+c.want {
				t.Errorf("week %v, want %d", e.Total, 2000+c.want)
			}
			var standby int64
			for _, r := range e.Reasons {
				if r.Basis == PayStandbyPassive || r.Basis == PayStandbyActive {
					standby += r.Amount
				}
			}
			if standby != c.want {
				t.Errorf("standby reasons %v", e.Reasons)
			}
		})
	}
	for name, bad := range map[string]PayRule{
		"passive per call-out": standby("x", PayStandbyPassive, 100, UnitAssignment),
		"per week":             standby("x", PayStandbyActive, 100, UnitWeek),
		"for a project":        func() PayRule { r := standby("x", PayStandbyActive, 100, ""); r.ProjectID = "p1"; return r }(),
	} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := (DayInfo{ID: DayInfoID("e1", d("2026-01-05")), Owner: owner, EmployerID: "e1", Date: d("2026-01-05"),
		Callouts: -1}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("negative call-outs: %v", err)
	}
}

// A day with only standby is not a working day for pay per day, and a plan day pays its plan, not its standby, by the
// hour.
func TestStandbyIsNoWorkingDay(t *testing.T) {
	perDay := rule("d", PayPerAssignment, 10000, "2026-01-01", "")
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeWeekSum, "")}, PayRules: []PayRule{perDay},
		DayInfos: []DayInfo{standbyDay("2026-01-05", 0, 120, 1)}}}
	if e := earnings(t, data, "2026-01-05", "2026-01-05"); e.Total["EUR"] != 0 {
		t.Errorf("standby alone earned %v", e.Total)
	}
	plan := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModePlan, "")},
		Schedules: []Schedule{week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)},
		PayRules:  []PayRule{rule("h", PayHourly, 2000, "2026-01-01", "")},
		DayInfos:  []DayInfo{standbyDay("2026-01-05", 0, 120, 1)}}}
	if e := earnings(t, plan, "2026-01-05", "2026-01-05"); e.Total["EUR"] != 16000 {
		t.Errorf("the plan day earned %v, want the plan's 8 hours", e.Total)
	}
}
