// SPDX-License-Identifier: MIT

package core

import (
	"testing"
	"time"
)

// The maintainer's example: Monday to Thursday 8.5 h, Friday 6 h.
func craftsman() Schedule { return week("s1", "2026-01-01", 510, 510, 510, 510, 360, 0, 0) }

func TestDaysSchedule(t *testing.T) {
	// Week of 2026-03-02 (Monday) in Bavaria, no holidays.
	cases := []struct {
		name     string
		entries  []Entry
		absences []Absence
		date     string
		want     Day
	}{
		{"exactly the target", []Entry{work("2026-03-02", 510)}, nil, "2026-03-02",
			Day{Target: 510, AdjustedTarget: 510, Worked: 510, Actual: 510, Balance: 0}},
		{"longer", []Entry{work("2026-03-03", 600)}, nil, "2026-03-03",
			Day{Target: 510, AdjustedTarget: 510, Worked: 600, Actual: 600, Balance: 90}},
		{"nothing booked", nil, nil, "2026-03-04",
			Day{Target: 510, AdjustedTarget: 510, Balance: -510}},
		{"break is taken off", []Entry{work("2026-03-05", 540), withKind(work("2026-03-05", 30), EntryBreak)}, nil,
			"2026-03-05", Day{Target: 510, AdjustedTarget: 510, Worked: 510, Breaks: 30, Actual: 510}},
		{"Friday is shorter", []Entry{work("2026-03-06", 360)}, nil, "2026-03-06",
			Day{Target: 360, AdjustedTarget: 360, Worked: 360, Actual: 360}},
		{"Saturday has no target", []Entry{work("2026-03-07", 120)}, nil, "2026-03-07",
			Day{Worked: 120, Actual: 120, Balance: 120}},
		{"vacation reduces the target", nil, []Absence{absence(AbsenceVacation, "2026-03-02", "2026-03-06")}, "2026-03-04",
			Day{Target: 510, Absences: []string{"vacation"}}},
		{"half a day of vacation", []Entry{work("2026-03-04", 255)},
			[]Absence{func() Absence { a := absence(AbsenceVacation, "2026-03-04", "2026-03-04"); a.HalfDay = true; return a }()},
			"2026-03-04", Day{Target: 510, AdjustedTarget: 255, Worked: 255, Actual: 255, Absences: []string{"vacation"}}},
		{"time off in lieu costs the balance", nil, []Absence{absence(AbsenceComp, "2026-03-04", "2026-03-04")},
			"2026-03-04", Day{Target: 510, AdjustedTarget: 510, Balance: -510, Absences: []string{"comp"}}},
		{"a requested absence does not count yet", nil,
			[]Absence{func() Absence {
				a := absence(AbsenceVacation, "2026-03-04", "2026-03-04")
				a.Status = StatusRequested
				return a
			}()},
			"2026-03-04", Day{Target: 510, AdjustedTarget: 510, Balance: -510}},
		{"a deleted entry does not count", []Entry{func() Entry { e := work("2026-03-04", 510); now := time.Now(); e.DeletedAt = &now; return e }()},
			nil, "2026-03-04", Day{Target: 510, AdjustedTarget: 510, Balance: -510}},
		{"an absence for every employer counts", nil,
			[]Absence{func() Absence { a := absence(AbsenceSick, "2026-03-04", "2026-03-04"); a.EmployerID = ""; return a }()},
			"2026-03-04", Day{Target: 510, Absences: []string{"sick"}}},
		{"an absence of another employer does not", nil,
			[]Absence{func() Absence { a := absence(AbsenceSick, "2026-03-04", "2026-03-04"); a.EmployerID = "e2"; return a }()},
			"2026-03-04", Day{Target: 510, AdjustedTarget: 510, Balance: -510}},
		{"two absences never take more than the day", nil, []Absence{absence(AbsenceSick, "2026-03-04", "2026-03-04"),
			absence(AbsenceVacation, "2026-03-04", "2026-03-04")}, "2026-03-04",
			Day{Target: 510, Absences: []string{"sick"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")},
				Schedules: []Schedule{craftsman()}}, Entries: c.entries, Absences: c.absences}
			got := dayOf(t, mustDays(t, data, "2026-03-02", "2026-03-08"), c.date)
			want := c.want
			want.Date, want.EmployerID, want.Mode, want.HasTarget = d(c.date), "e1", ModeSchedule, true
			assertDay(t, got, want)
		})
	}
}

func assertDay(t *testing.T, got, want Day) {
	t.Helper()
	if got.Target != want.Target || got.AdjustedTarget != want.AdjustedTarget || got.Worked != want.Worked ||
		got.Breaks != want.Breaks || got.Credited != want.Credited || got.Actual != want.Actual ||
		got.Balance != want.Balance || got.Mode != want.Mode || got.HasTarget != want.HasTarget ||
		got.FromPlan != want.FromPlan || got.Confirmed != want.Confirmed || len(got.Absences) != len(want.Absences) {
		t.Fatalf("%s:\n got  %+v\n want %+v", want.Date, got, want)
	}
	for i := range want.Absences {
		if got.Absences[i] != want.Absences[i] {
			t.Fatalf("absences %v, want %v", got.Absences, want.Absences)
		}
	}
}

func TestAbsenceThatFillsTheTarget(t *testing.T) {
	cat := Catalog{Employers: []Employer{employerOf(ModeSchedule, "")}, Schedules: []Schedule{craftsman()},
		AbsenceTypes: []AbsenceType{{ID: "x", Owner: owner, Key: AbsenceSick, Name: "Sick", Effect: FillsTarget, Health: true}}}
	got := dayOf(t, mustDays(t, Data{Catalog: cat, Absences: []Absence{absence(AbsenceSick, "2026-03-04", "2026-03-04")}},
		"2026-03-04", "2026-03-04"), "2026-03-04")
	// Sickness that counts as worked: the target stays, the time is credited, the balance is even.
	assertDay(t, got, Day{Date: d("2026-03-04"), Mode: ModeSchedule, HasTarget: true, Target: 510, AdjustedTarget: 510,
		Credited: 510, Actual: 510, Absences: []string{"sick"}})
}

func TestPublicHolidayReducesTheTarget(t *testing.T) {
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")}, Schedules: []Schedule{craftsman()}},
		Entries: []Entry{work("2026-01-06", 120)}}
	days := mustDays(t, data, "2026-01-05", "2026-01-09")
	got := dayOf(t, days, "2026-01-06") // Epiphany in Bavaria
	if got.Holiday != "Epiphany" || got.AdjustedTarget != 0 || got.Balance != 120 {
		t.Fatalf("holiday: %+v", got)
	}
	// In Berlin it is a normal Tuesday.
	data.Catalog.Employers[0].Region = "DE-BE"
	got = dayOf(t, mustDays(t, data, "2026-01-05", "2026-01-09"), "2026-01-06")
	if got.Holiday != "" || got.AdjustedTarget != 510 {
		t.Fatalf("Berlin: %+v", got)
	}
}

func TestPartTime(t *testing.T) {
	// Monday and Wednesday, 4 h each.
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")},
		Schedules: []Schedule{week("p", "2026-01-01", 240, 0, 240, 0, 0, 0, 0)}},
		Entries: []Entry{work("2026-03-02", 240), work("2026-03-04", 300), work("2026-03-05", 60)}}
	tot := Sum(mustDays(t, data, "2026-03-02", "2026-03-08"))
	if tot.Target != 480 || tot.Worked != 600 || tot.Balance != 120 {
		t.Fatalf("part-time week: %+v", tot)
	}
}

func TestScheduleChangesInTheMiddleOfAMonth(t *testing.T) {
	// 40 h until 2026-03-15, 20 h (Monday to Friday 4 h) from Monday 2026-03-16. Nothing is booked.
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "")}, Schedules: []Schedule{
		week("late", "2026-03-16", 240, 240, 240, 240, 240, 0, 0),
		week("early", "2026-01-01", 480, 480, 480, 480, 480, 0, 0),
	}}}
	days := mustDays(t, data, "2026-03-01", "2026-03-31")
	// March 2026: 10 working days before the 16th (2nd to 13th), 12 from the 16th (16th to 31st).
	tot := Sum(days)
	if want := 10*480 + 12*240; tot.Target != want || tot.Balance != -want {
		t.Fatalf("March target %d balance %d, want %d", tot.Target, tot.Balance, want)
	}
	if dayOf(t, days, "2026-03-13").Target != 480 || dayOf(t, days, "2026-03-16").Target != 240 {
		t.Fatal("the change is not on the 16th")
	}
	// Earlier months stay as they were.
	feb := Sum(mustDays(t, data, "2026-02-01", "2026-02-28"))
	if feb.Target != 20*480 {
		t.Fatalf("February target %d", feb.Target)
	}
}

func TestTotalsAcrossMonthEnds(t *testing.T) {
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")}, Schedules: []Schedule{craftsman()}}}
	for _, date := range []string{"2026-09-28", "2026-09-29", "2026-09-30", "2026-10-01", "2026-10-02"} {
		data.Entries = append(data.Entries, work(date, 540))
	}
	days := mustDays(t, data, "2026-09-28", "2026-10-04")
	weeks := Totals(days, PeriodWeek, time.Monday)
	months := Totals(days, PeriodMonth, time.Monday)
	if len(weeks) != 1 || weeks[0].Worked != 5*540 || weeks[0].Target != 4*510+360 {
		t.Fatalf("week: %+v", weeks)
	}
	if len(months) != 2 || months[0].To != d("2026-09-30") || months[1].From != d("2026-10-01") {
		t.Fatalf("months: %+v", months)
	}
	// September: Mon to Wed; October: Thu, Fri, and the weekend.
	if months[0].Balance != 3*30 || months[1].Balance != 30+180 {
		t.Fatalf("balances: September %d, October %d", months[0].Balance, months[1].Balance)
	}
	if months[0].Balance+months[1].Balance != weeks[0].Balance {
		t.Fatal("months and week disagree")
	}
	// A week starting on Sunday splits differently.
	if got := Totals(days, PeriodWeek, time.Sunday); len(got) != 2 || got[1].From != d("2026-10-04") {
		t.Fatalf("Sunday weeks: %+v", got)
	}
	if got := Totals(days, PeriodYear, time.Monday); len(got) != 1 || got[0].Days != 7 {
		t.Fatalf("year: %+v", got)
	}
	if got := Totals(days, PeriodDay, time.Monday); len(got) != 7 {
		t.Fatalf("days: %d", len(got))
	}
}

func TestModeWeekSumAndProjectsOnly(t *testing.T) {
	for _, mode := range []Mode{ModeWeekSum, ModeProjectsOnly} {
		e1 := work("2026-03-02", 38*60+30)
		e1.Precision, e1.ProjectID = PrecisionWeek, "p1"
		e2 := work("2026-03-03", 60)
		e2.ProjectID = "p2"
		data := Data{Catalog: Catalog{Employers: []Employer{employerOf(mode, "DE-BY")}, Schedules: []Schedule{craftsman()}},
			Entries: []Entry{e1, e2}}
		days := mustDays(t, data, "2026-03-02", "2026-03-15")
		tot := Sum(days)
		if tot.Target != 0 || tot.Balance != 0 || tot.Worked != 39*60+30 {
			t.Fatalf("%s: %+v", mode, tot)
		}
		if tot.Projects["p1"] != 38*60+30 || tot.Projects["p2"] != 60 {
			t.Fatalf("%s projects: %v", mode, tot.Projects)
		}
		// The next week starts empty.
		next := Totals(days, PeriodWeek, time.Monday)
		if len(next) != 2 || next[1].Worked != 0 || next[1].Balance != 0 {
			t.Fatalf("%s next week: %+v", mode, next)
		}
	}
}

func TestModePlan(t *testing.T) {
	adjust := func(date string, minutes int) Entry { return withKind(work(date, minutes), EntryAdjust) }
	cases := []struct {
		name     string
		entries  []Entry
		absences []Absence
		date     string
		want     Day
	}{
		{"the plan counts as worked", nil, nil, "2026-03-02",
			Day{Target: 510, AdjustedTarget: 510, Worked: 510, Actual: 510, FromPlan: true, Confirmed: true}},
		{"left two hours early", []Entry{adjust("2026-03-06", -120)}, nil, "2026-03-06",
			Day{Target: 360, AdjustedTarget: 360, Worked: 240, Actual: 240, Balance: -120, FromPlan: true}},
		{"stayed longer", []Entry{adjust("2026-03-04", 90)}, nil, "2026-03-04",
			Day{Target: 510, AdjustedTarget: 510, Worked: 600, Actual: 600, Balance: 90, FromPlan: true, Confirmed: true}},
		{"sick", nil, []Absence{absence(AbsenceSick, "2026-03-03", "2026-03-03")}, "2026-03-03",
			Day{Target: 510, Absences: []string{"sick"}, Confirmed: true}},
		{"half a day of vacation", nil,
			[]Absence{func() Absence { a := absence(AbsenceVacation, "2026-03-03", "2026-03-03"); a.HalfDay = true; return a }()},
			"2026-03-03", Day{Target: 510, AdjustedTarget: 255, Worked: 255, Actual: 255, FromPlan: true, Confirmed: true,
				Absences: []string{"vacation"}}},
		{"time off in lieu is missing from the balance", nil, []Absence{absence(AbsenceComp, "2026-03-03", "2026-03-03")},
			"2026-03-03", Day{Target: 510, AdjustedTarget: 510, Balance: -510, Absences: []string{"comp"}, Confirmed: true}},
		{"booked work replaces the plan", []Entry{work("2026-03-05", 300)}, nil, "2026-03-05",
			Day{Target: 510, AdjustedTarget: 510, Worked: 300, Actual: 300, Balance: -210}},
		{"work on a free day", []Entry{work("2026-03-07", 180)}, nil, "2026-03-07",
			Day{Worked: 180, Actual: 180, Balance: 180}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModePlan, "DE-BY")}, Schedules: []Schedule{craftsman()},
				Confirmations: []Confirmation{{ID: "c", Owner: owner, EmployerID: "e1", From: d("2026-03-02"), To: d("2026-03-04")}}},
				Entries: c.entries, Absences: c.absences}
			got := dayOf(t, mustDays(t, data, "2026-03-02", "2026-03-08"), c.date)
			want := c.want
			want.Date, want.EmployerID, want.Mode, want.HasTarget = d(c.date), "e1", ModePlan, true
			assertDay(t, got, want)
		})
	}
	// A public holiday is no plan day.
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModePlan, "DE-BY")}, Schedules: []Schedule{craftsman()}}}
	if got := dayOf(t, mustDays(t, data, "2026-01-06", "2026-01-06"), "2026-01-06"); got.Worked != 0 || got.FromPlan || got.Balance != 0 {
		t.Fatalf("holiday in plan mode: %+v", got)
	}
}

// Switching modes changes the view, never the data: back in the first mode, every number is the same again.
func TestModeSwitchKeepsData(t *testing.T) {
	entries := []Entry{work("2026-03-02", 540), work("2026-03-03", 480), withKind(work("2026-03-03", 30), EntryBreak),
		withKind(work("2026-03-04", 60), EntryAdjust)}
	base := Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")}, Schedules: []Schedule{craftsman()}}
	before := mustDays(t, Data{Catalog: base, Entries: entries}, "2026-03-01", "2026-03-31")

	switched := base
	switched.ModeChanges = []ModeChange{
		{ID: "m1", Owner: owner, EmployerID: "e1", ValidFrom: d("2026-03-03"), Mode: ModeWeekSum},
		{ID: "m2", Owner: owner, EmployerID: "e1", ValidFrom: d("2026-03-10"), Mode: ModeProjectsOnly},
	}
	mid := mustDays(t, Data{Catalog: switched, Entries: entries}, "2026-03-01", "2026-03-31")
	if dayOf(t, mid, "2026-03-02").Mode != ModeSchedule || dayOf(t, mid, "2026-03-03").Mode != ModeWeekSum ||
		dayOf(t, mid, "2026-03-20").Mode != ModeProjectsOnly {
		t.Fatal("modes are not taken per day")
	}
	if Sum(mid).Worked != Sum(before).Worked {
		t.Fatalf("worked time changed with the mode: %d != %d", Sum(mid).Worked, Sum(before).Worked)
	}
	switched.ModeChanges = append(switched.ModeChanges,
		ModeChange{ID: "m3", Owner: owner, EmployerID: "e1", ValidFrom: d("2026-03-01"), Mode: ModeSchedule})
	switched.ModeChanges = switched.ModeChanges[2:]
	after := mustDays(t, Data{Catalog: switched, Entries: entries}, "2026-03-01", "2026-03-31")
	if Sum(after).Balance != Sum(before).Balance || Sum(after).Worked != Sum(before).Worked {
		t.Fatalf("back in the first mode: %+v != %+v", Sum(after), Sum(before))
	}
	if got := ModeAt(switched, "e1", d("2026-02-01")); got != ModeSchedule {
		t.Fatalf("ModeAt before any change: %s", got)
	}
}

// From plan to exact: the plan days become entries, and every day keeps its minutes.
func TestMaterializePlan(t *testing.T) {
	start, end := 7*60, 16*60
	sched := craftsman()
	sched.Days[time.Monday] = DayPlan{Minutes: 510, Start: &start, End: &end, Break: 30}
	cat := Catalog{Employers: []Employer{employerOf(ModePlan, "DE-BY")}, Schedules: []Schedule{sched}}
	entries := []Entry{withKind(work("2026-03-25", -120), EntryAdjust), work("2026-03-26", 300)}
	absences := []Absence{absence(AbsenceSick, "2026-03-24", "2026-03-24"),
		func() Absence { a := absence(AbsenceVacation, "2026-03-27", "2026-03-27"); a.HalfDay = true; return a }()}
	// The week of the change to summer time, 2026-03-29.
	from, to := "2026-03-23", "2026-03-31"
	plan := mustDays(t, Data{Catalog: cat, Entries: entries, Absences: absences}, from, to)

	made, err := MaterializePlan(Data{Catalog: cat, Entries: entries, Absences: absences}, "e1", d(from), d(to))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range made {
		e.ID = NewID()
		if err := e.Normalize(); err != nil {
			t.Fatal(err)
		}
		if err := e.Validate(); err != nil {
			t.Fatalf("materialized entry invalid: %v %+v", err, e)
		}
		entries = append(entries, e)
	}
	exact := cat
	exact.ModeChanges = []ModeChange{{ID: "m", Owner: owner, EmployerID: "e1", ValidFrom: d(from), Mode: ModeExact}}
	got := mustDays(t, Data{Catalog: exact, Entries: entries, Absences: absences}, from, to)
	for i := range plan {
		if plan[i].Worked != got[i].Worked || plan[i].Balance != got[i].Balance {
			t.Errorf("%s: plan %d/%d, exact %d/%d", plan[i].Date, plan[i].Worked, plan[i].Balance, got[i].Worked, got[i].Balance)
		}
	}
	// Monday 2026-03-30, after the change to summer time, has a span 07:00 to 16:00 local and a break.
	var span, brk *Entry
	for i := range made {
		if made[i].Date == d("2026-03-30") && made[i].Kind == EntryWork {
			span = &made[i]
		}
		if made[i].Date == d("2026-03-30") && made[i].Kind == EntryBreak {
			brk = &made[i]
		}
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	if span == nil || brk == nil || span.Start.In(berlin).Hour() != 7 || span.End.In(berlin).Hour() != 16 || brk.Minutes != 30 {
		t.Fatalf("Monday span %+v break %+v", span, brk)
	}
	for _, e := range made {
		if e.Date == d("2026-03-24") || e.Date == d("2026-03-26") {
			t.Fatalf("an entry for a day without plan time: %+v", e)
		}
	}
}

func TestSpreadWeek(t *testing.T) {
	e := work("2026-03-02", 2000)
	e.Precision = PrecisionWeek
	cat := Catalog{Employers: []Employer{employerOf(ModeWeekSum, "")}, Schedules: []Schedule{craftsman()}}
	parts, err := SpreadWeek(cat, e)
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, p := range parts {
		sum += p.Minutes
	}
	if len(parts) != 5 || sum != 2000 || parts[4].Date != d("2026-03-06") || parts[4].Minutes >= parts[0].Minutes {
		t.Fatalf("spread: %+v", parts)
	}
	parts, err = SpreadWeek(Catalog{}, e)
	if err != nil || len(parts) != 5 || parts[0].Minutes != 400 {
		t.Fatalf("spread without schedule: %+v %v", parts, err)
	}
	if _, err := SpreadWeek(cat, work("2026-03-02", 60)); err == nil {
		t.Fatal("spread a day entry")
	}
}

func TestDaysErrors(t *testing.T) {
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "")}}}
	if _, err := Days(data, "nobody", d("2026-01-01"), d("2026-01-02")); err == nil {
		t.Fatal("unknown employer")
	}
	if _, err := Days(data, "e1", d("2026-01-02"), d("2026-01-01")); err == nil {
		t.Fatal("reversed range")
	}
	// Without a schedule, a mode with a target has a target of zero.
	if got := mustDays(t, Data{Catalog: data.Catalog, Entries: []Entry{work("2026-01-02", 60)}}, "2026-01-02", "2026-01-02"); got[0].Balance != 60 {
		t.Fatalf("no schedule: %+v", got[0])
	}
}
