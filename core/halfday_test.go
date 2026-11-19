// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"testing"
)

func partOf(e Entry, p DayPart) Entry {
	e.Part = p
	return e
}

// A ski instructor paid per day: a whole day, a morning, an afternoon. A morning and an afternoon of the same day make
// a whole day; work without a half makes the day whole.
func TestHalfDayPart(t *testing.T) {
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeWeekSum, "")}},
		Entries: []Entry{
			work("2026-01-05", 360),
			partOf(work("2026-01-06", 180), Morning),
			partOf(work("2026-01-07", 180), Afternoon),
			partOf(work("2026-01-08", 180), Morning), partOf(work("2026-01-08", 180), Afternoon),
			partOf(work("2026-01-09", 180), Morning), work("2026-01-09", 60),
			partOf(work("2026-01-10", 0), Morning),
		}}
	days := mustDays(t, data, "2026-01-05", "2026-01-10")
	for date, want := range map[string]DayPart{"2026-01-05": "", "2026-01-06": Morning, "2026-01-07": Afternoon,
		"2026-01-08": "", "2026-01-09": "", "2026-01-10": ""} {
		if got := dayOf(t, days, date).Part; got != want {
			t.Errorf("%s: part %q, want %q", date, got, want)
		}
	}
}

// Pay per day with an own amount for half a day, and without one half the amount; the week and the lines add up.
func TestHalfDayPay(t *testing.T) {
	perDay := rule("d", PayPerAssignment, 12000, "2026-01-01", "")
	perDay.Unit = UnitDay
	entries := []Entry{
		work("2026-01-05", 360),
		partOf(work("2026-01-06", 180), Morning),
		partOf(work("2026-01-07", 180), Afternoon),
		partOf(work("2026-01-08", 180), Morning), partOf(work("2026-01-08", 180), Afternoon),
	}
	cases := []struct {
		name string
		half int64
		want int64
	}{
		{"half of the amount", 0, 12000 + 6000 + 6000 + 12000},
		{"an own amount for half a day", 7000, 12000 + 7000 + 7000 + 12000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := perDay
			r.HalfAmount = c.half
			if err := r.Validate(); err != nil {
				t.Fatal(err)
			}
			data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeWeekSum, "")}, PayRules: []PayRule{r}},
				Entries: entries}
			e := earnings(t, data, "2026-01-05", "2026-01-11")
			if e.Total["EUR"] != c.want {
				t.Fatalf("week %v, want %d", e.Total, c.want)
			}
			halves := 0
			for _, l := range e.Lines {
				if l.HalfDay {
					halves += l.Quantity
					if l.Rate != r.HalfDayAmount() {
						t.Errorf("half day rate %d", l.Rate)
					}
				}
			}
			if halves != 2 {
				t.Errorf("%d half days in the lines %+v", halves, e.Lines)
			}
			if day := earnings(t, data, "2026-01-06", "2026-01-06"); day.Total["EUR"] != r.HalfDayAmount() {
				t.Errorf("the morning earns %v", day.Total)
			}
		})
	}
	for name, change := range map[string]func(*PayRule){
		"negative":        func(r *PayRule) { r.HalfAmount = -1 },
		"per job":         func(r *PayRule) { r.HalfAmount, r.Unit = 100, UnitAssignment },
		"hourly":          func(r *PayRule) { r.HalfAmount, r.Basis, r.Unit = 100, PayHourly, "" },
		"standby per day": func(r *PayRule) { r.HalfAmount, r.Basis = 100, PayStandbyPassive },
	} {
		bad := perDay
		change(&bad)
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := work("2026-01-05", 60)
	bad.Part = "noon"
	if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown part: %v", err)
	}
	if err := partOf(withKind(work("2026-01-05", 30), EntryBreak), Morning).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("a break with a part: %v", err)
	}
}

// Half a day of leave and half a day of work on the same day: the leave covers half the target, the hours count, and
// the work pays half a day.
func TestHalfLeaveAndHalfWork(t *testing.T) {
	perDay := rule("d", PayPerAssignment, 12000, "2026-01-01", "")
	leave := absence(AbsenceVacation, "2026-01-06", "2026-01-06")
	leave.HalfDay = true
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "")},
		Schedules: []Schedule{week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)}, PayRules: []PayRule{perDay}},
		Entries: []Entry{partOf(work("2026-01-06", 240), Afternoon)}, Absences: []Absence{leave}}
	day := dayOf(t, mustDays(t, data, "2026-01-06", "2026-01-06"), "2026-01-06")
	if day.Credited+day.Target-day.AdjustedTarget != 240 || day.Worked != 240 || day.Balance != 0 || day.Part != Afternoon {
		t.Fatalf("day %+v", day)
	}
	if e := earnings(t, data, "2026-01-06", "2026-01-06"); e.Total["EUR"] != 6000 {
		t.Errorf("earned %v", e.Total)
	}
}
