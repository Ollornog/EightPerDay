// SPDX-License-Identifier: MIT

package core

import "testing"

func TestParseDayCode(t *testing.T) {
	cases := []struct {
		in   string
		want DayCode
		ok   bool
	}{
		{"U", DayCode{AbsenceVacation, false}, true},
		{"u", DayCode{AbsenceVacation, false}, true},
		{" V ", DayCode{AbsenceVacation, false}, true},
		{"K", DayCode{AbsenceSick, false}, true},
		{"s", DayCode{AbsenceSick, false}, true},
		{"F", DayCode{AbsenceHoliday, false}, true},
		{"O", DayCode{AbsenceHoliday, false}, true},
		{"ZA", DayCode{AbsenceComp, false}, true},
		{"za", DayCode{AbsenceComp, false}, true},
		{"½U", DayCode{AbsenceVacation, true}, true},
		{"U½", DayCode{AbsenceVacation, true}, true},
		{"U/2", DayCode{AbsenceVacation, true}, true},
		{"0,5U", DayCode{AbsenceVacation, true}, true},
		{"0.5 u", DayCode{AbsenceVacation, true}, true},
		{",5F", DayCode{AbsenceHoliday, true}, true},
		{"8", DayCode{}, false},
		{"8,5", DayCode{}, false},
		{"0,5", DayCode{}, false},
		{"½", DayCode{}, false},
		{"", DayCode{}, false},
		{"UU", DayCode{}, false},
		{"H", DayCode{}, false}, // public holidays come from the region, never typed
		{"Urlaub", DayCode{}, false},
		{"Z A", DayCode{}, false},
		{"½U½", DayCode{}, false},
	}
	for _, c := range cases {
		got, ok := ParseDayCode(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseDayCode(%q) = %+v %v, want %+v %v", c.in, got, ok, c.want, c.ok)
		}
	}
	for _, key := range []string{AbsenceVacation, AbsenceSick, AbsenceHoliday, AbsenceComp} {
		if !CodedAbsence(key) {
			t.Errorf("%s has a code", key)
		}
	}
	if CodedAbsence(AbsenceUnpaid) || CodedAbsence("parental") {
		t.Error("unpaid leave and own types have no code")
	}
}

func TestLedger(t *testing.T) {
	// 8 h Monday to Friday from Monday 2 March 2026 on. Week 1 (2–8 March): 41 h worked, +1 h. Week 2 (9–15 March):
	// Monday sick, Tuesday 9 h, Wednesday a half day of leave and 4 h, then nothing; until is Wednesday the 11th. A
	// payout of 30 minutes on 10 March, a carry-over before the account starts that counts for nothing.
	sick := absence(AbsenceSick, "2026-03-09", "2026-03-09")
	halfLeave := absence(AbsenceVacation, "2026-03-11", "2026-03-11")
	halfLeave.HalfDay = true
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "")},
		Schedules: []Schedule{week("s", "2026-03-02", 480, 480, 480, 480, 480, 0, 0)},
		Adjustments: []Adjustment{
			{ID: "a1", Owner: owner, EmployerID: "e1", Date: d("2026-03-10"), Kind: AdjustOvertime, Minutes: -30, Reason: "payout"},
			{ID: "a2", Owner: owner, EmployerID: "e1", Date: d("2026-02-01"), Kind: AdjustOvertime, Minutes: 999, Reason: "before"},
			{ID: "a3", Owner: owner, EmployerID: "e1", Date: d("2026-03-12"), Kind: AdjustOvertime, Minutes: 77, Reason: "after until"},
		}},
		Entries: []Entry{
			work("2026-03-02", 540), work("2026-03-03", 480), work("2026-03-04", 480), work("2026-03-05", 480), work("2026-03-06", 480),
			work("2026-03-10", 540), work("2026-03-11", 240), work("2026-03-13", 600),
		},
		Absences: []Absence{sick, halfLeave}}
	until := d("2026-03-11")
	weeks, err := Ledger(data, "e1", d("2026-03-02"), d("2026-03-04"), d("2026-03-12"), until)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 2 || weeks[0].From != d("2026-03-02") || weeks[0].To != d("2026-03-08") || weeks[1].From != d("2026-03-09") {
		t.Fatalf("weeks %+v", weeks)
	}
	w1, w2 := weeks[0], weeks[1]
	if len(w1.Days) != 7 || w1.CarryOver != 0 || w1.Total.Worked != 2460 || w1.Total.Balance != 60 || w1.Balance != 60 {
		t.Errorf("week 1: carry %d worked %d balance %d/%d", w1.CarryOver, w1.Total.Worked, w1.Total.Balance, w1.Balance)
	}
	// Sick Monday meets the target, Tuesday +1 h, Wednesday 4 h against 4 h, then -0:30 paid out; Friday's ten hours
	// lie after until and count for nothing yet.
	if w2.CarryOver != 60 || w2.Total.Worked != 780 || w2.Total.Balance != 60 || w2.Balance != 60+60-30 {
		t.Errorf("week 2: carry %d worked %d balance %d/%d", w2.CarryOver, w2.Total.Worked, w2.Total.Balance, w2.Balance)
	}
	if len(w2.Corrections) != 1 || w2.Corrections[0].Reason != "payout" {
		t.Errorf("corrections %+v", w2.Corrections)
	}
	if day := dayOf(t, w2.Days, "2026-03-13"); day.Worked != 600 {
		t.Errorf("a day after until is still listed: %+v", day)
	}
	acc, err := Overtime(data, "e1", d("2026-03-02"), until)
	if err != nil || acc.Balance != w2.Balance {
		t.Errorf("the ledger's balance %d differs from the account %+v %v", w2.Balance, acc, err)
	}
	// A list that starts later carries the balance of the weeks before it.
	later, err := Ledger(data, "e1", d("2026-03-02"), d("2026-03-10"), d("2026-03-10"), until)
	if err != nil || len(later) != 1 || later[0].CarryOver != 60 || later[0].Balance != w2.Balance {
		t.Errorf("later list %+v %v", later, err)
	}
	if _, err := Ledger(data, "nobody", d("2026-03-02"), d("2026-03-02"), d("2026-03-02"), until); err == nil {
		t.Error("an unknown employer is refused")
	}
	if _, err := Ledger(data, "e1", d("2026-03-02"), d("2026-03-16"), d("2026-03-02"), until); err == nil {
		t.Error("a range that ends before it starts is refused")
	}
}

func TestWeekGaps(t *testing.T) {
	// 8 h Monday to Friday from 2 March 2026; today is Wednesday 18 March.
	plan := func(mode Mode) Data {
		return Data{Catalog: Catalog{Employers: []Employer{employerOf(mode, "DE-BY")},
			Schedules: []Schedule{week("s", "2026-03-02", 480, 480, 480, 480, 480, 0, 0)}}}
	}
	today := d("2026-03-18")

	// Schedule: week 1 complete, week 2 lacks Tuesday and Thursday (Monday sick, Wednesday a day off), this week lacks
	// Monday; Tuesday is booked and today and the days after it lack nothing yet.
	data := plan(ModeSchedule)
	data.Entries = []Entry{work("2026-03-02", 480), work("2026-03-03", 480), work("2026-03-04", 480), work("2026-03-05", 480),
		work("2026-03-06", 480), work("2026-03-11", 480), work("2026-03-13", 300), work("2026-03-17", 480)}
	data.Absences = []Absence{absence(AbsenceSick, "2026-03-09", "2026-03-09"), absence(AbsenceHoliday, "2026-03-11", "2026-03-11")}
	data.Entries = append(data.Entries[:5], data.Entries[6:]...) // nothing on 11 March but the day off
	gaps, err := WeekGaps(data, "e1", d("2026-03-02"), d("2026-02-01"), today, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 2 || gaps[0].From != d("2026-03-09") || len(gaps[0].Missing) != 2 ||
		gaps[0].Missing[0] != d("2026-03-10") || gaps[0].Missing[1] != d("2026-03-12") {
		t.Fatalf("gaps %+v", gaps)
	}
	if gaps[1].From != d("2026-03-16") || len(gaps[1].Missing) != 1 || gaps[1].Missing[0] != d("2026-03-16") || gaps[1].NotSubmitted {
		t.Errorf("this week %+v", gaps[1])
	}

	// Approval: every week that is over and not handed in, even when complete; a handed-in week lacks nothing.
	data.Catalog.Submissions = []Submission{{ID: "p1", Owner: owner, EmployerID: "e1", Kind: SubmitPeriod, From: d("2026-03-02"),
		To: d("2026-03-08"), Status: SubmissionSubmitted}}
	gaps, _ = WeekGaps(data, "e1", d("2026-03-02"), d("2026-02-01"), today, true)
	if len(gaps) != 2 || !gaps[0].NotSubmitted || gaps[1].NotSubmitted {
		t.Errorf("approval gaps %+v", gaps)
	}

	// Plan: the plan counts, so nothing is missing; a week that is over and not confirmed is.
	data = plan(ModePlan)
	data.Catalog.Confirmations = []Confirmation{{ID: "c1", Owner: owner, EmployerID: "e1", From: d("2026-03-02"), To: d("2026-03-08")}}
	gaps, _ = WeekGaps(data, "e1", d("2026-03-02"), d("2026-02-01"), today, false)
	if len(gaps) != 1 || gaps[0].From != d("2026-03-09") || !gaps[0].Unconfirmed || len(gaps[0].Missing) != 0 {
		t.Errorf("plan gaps %+v", gaps)
	}

	// Without a target nothing is lacking, and nothing before the account's start.
	if gaps, _ := WeekGaps(plan(ModeWeekSum), "e1", d("2026-03-02"), d("2026-02-01"), today, true); len(gaps) != 0 {
		t.Errorf("week sum gaps %+v", gaps)
	}
	if gaps, _ := WeekGaps(plan(ModeSchedule), "e1", d("2026-03-16"), d("2026-02-01"), today, false); len(gaps) != 1 ||
		gaps[0].From != d("2026-03-16") {
		t.Errorf("before the start %+v", gaps)
	}
}
