// SPDX-License-Identifier: MIT

package core

import "testing"

func rule(id string, basis PayBasis, amount int64, from, to string) PayRule {
	r := PayRule{ID: id, Owner: owner, EmployerID: "e1", ValidFrom: d(from), Basis: basis, Amount: amount, Currency: "EUR"}
	if to != "" {
		r.ValidTo = d(to)
	}
	return r
}

func earnings(t *testing.T, data Data, from, to string) Earnings {
	t.Helper()
	got, err := ComputeEarnings(data, "e1", d(from), d(to))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestEarningsHourly(t *testing.T) {
	cat := Catalog{Employers: []Employer{employerOf(ModeExact, "")}, PayRules: []PayRule{
		rule("old", PayHourly, 2000, "2026-01-01", "2026-03-15"),
		rule("new", PayHourly, 2400, "2026-03-16", ""),
		func() PayRule { r := rule("proj", PayHourly, 6000, "2026-01-01", ""); r.ProjectID = "p1"; return r }(),
	}}
	project := work("2026-03-17", 60)
	project.ProjectID = "p1"
	data := Data{Catalog: cat, Entries: []Entry{
		work("2026-03-02", 480), // old rate
		work("2026-03-16", 480), // new rate from the 16th
		project,                 // the project's rate beats the employer's
		work("2026-03-18", 510), // with a break:
		withKind(work("2026-03-18", 30), EntryBreak),
	}}
	got := earnings(t, data, "2026-03-01", "2026-03-31")
	// 8 h at 20.00, 8 h + 8 h at 24.00, 1 h at 60.00.
	if want := int64(8*2000 + 16*2400 + 6000); got.Total["EUR"] != want {
		t.Fatalf("total %d, want %d: %+v", got.Total["EUR"], want, got.Lines)
	}
	if len(got.Lines) != 3 {
		t.Fatalf("lines: %+v", got.Lines)
	}

	// The snapshot keeps the rate an entry was booked with, until it is revalued on purpose.
	snap := work("2026-03-20", 60)
	ApplyRate(Catalog{PayRules: []PayRule{rule("then", PayHourly, 1000, "2026-01-01", "")}}, &snap)
	if snap.Rate == nil || snap.Rate.Amount != 1000 {
		t.Fatalf("snapshot: %+v", snap.Rate)
	}
	data.Entries = []Entry{snap}
	if got := earnings(t, data, "2026-03-20", "2026-03-20"); got.Total["EUR"] != 1000 {
		t.Fatalf("snapshot ignored: %+v", got)
	}
	changed := Revalue(cat, data.Entries)
	if len(changed) != 1 || changed[0].Rate.Amount != 2400 || changed[0].Rate.RuleID != "new" {
		t.Fatalf("revalue: %+v", changed)
	}
	if again := Revalue(cat, changed); len(again) != 0 {
		t.Fatalf("revalue twice: %+v", again)
	}
}

func TestEarningsPlanDaysAndDeviations(t *testing.T) {
	cat := Catalog{Employers: []Employer{employerOf(ModePlan, "DE-BY")}, Schedules: []Schedule{craftsman()},
		PayRules: []PayRule{rule("h", PayHourly, 3000, "2026-01-01", "")}}
	data := Data{Catalog: cat, Entries: []Entry{withKind(work("2026-03-06", -120), EntryAdjust)}}
	got := earnings(t, data, "2026-03-02", "2026-03-08")
	// 4 x 8.5 h + 6 h - 2 h = 38 h at 30.00.
	if got.Total["EUR"] != 38*3000 {
		t.Fatalf("plan week: %+v", got)
	}
}

func TestEarningsOtherBases(t *testing.T) {
	assignmentRule := rule("job", PayPerAssignment, 15000, "2026-01-01", "")
	assignmentRule.Unit = UnitAssignment
	projectRule := rule("fixed", PayPerProject, 500000, "2026-01-01", "")
	projectRule.ProjectID = "p1"
	cat := Catalog{Employers: []Employer{employerOf(ModeProjectsOnly, "")}, PayRules: []PayRule{
		rule("day", PayPerAssignment, 20000, "2026-01-01", ""),
		assignmentRule,
		projectRule,
	}, Projects: []Project{{ID: "p1", Owner: owner, Name: "Roof", CompletedOn: d("2026-03-20")},
		{ID: "p2", Owner: owner, Name: "Open", CompletedOn: d("2026-04-02")}}}
	a1 := work("2026-03-02", 300)
	a1.Assignment = "Smith"
	a2 := work("2026-03-03", 200)
	a2.Assignment = "Smith"
	a3 := work("2026-03-03", 100)
	a3.Assignment = "Jones"
	data := Data{Catalog: cat, Entries: []Entry{a1, a2, a3, work("2026-03-10", 0)}}
	got := earnings(t, data, "2026-03-01", "2026-03-31")
	// Days worked: 2nd and 3rd (the 10th has zero minutes): 2 x 200.00. Jobs: Smith, Jones: 2 x 150.00. Project: 5000.00.
	if want := int64(2*20000 + 2*15000 + 500000); got.Total["EUR"] != want {
		t.Fatalf("total %d, want %d: %+v", got.Total["EUR"], want, got.Lines)
	}
	if got := earnings(t, data, "2026-04-01", "2026-04-30"); got.Total["EUR"] != 0 {
		t.Fatalf("April has neither work nor a completed project with a rule: %+v", got)
	}
}

func TestEarningsFlatPeriod(t *testing.T) {
	monthly := rule("salary", PayFlatPeriod, 300000, "2026-02-15", "")
	cat := Catalog{Employers: []Employer{employerOf(ModeSchedule, "")}, PayRules: []PayRule{monthly}}
	cases := []struct {
		from, to string
		want     int64
	}{
		{"2026-03-01", "2026-03-31", 300000},
		{"2026-03-01", "2026-04-30", 600000},
		// From 15 February: 14 of 28 days.
		{"2026-02-01", "2026-02-28", 150000},
		// Half of March asked for: 16 of 31 days.
		{"2026-03-01", "2026-03-16", (16*300000 + 15) / 31},
		{"2026-01-01", "2026-01-31", 0},
	}
	for _, c := range cases {
		if got := earnings(t, Data{Catalog: cat}, c.from, c.to); got.Total["EUR"] != c.want {
			t.Errorf("%s to %s: %d, want %d (%+v)", c.from, c.to, got.Total["EUR"], c.want, got.Lines)
		}
	}
	yearly := rule("y", PayFlatPeriod, 3650000, "2026-01-01", "2026-12-31")
	yearly.Unit = UnitYear
	if got := earnings(t, Data{Catalog: Catalog{Employers: cat.Employers, PayRules: []PayRule{yearly}}}, "2026-01-01", "2026-01-10"); got.Total["EUR"] != 100000 {
		t.Fatalf("ten days of a year: %+v", got)
	}
	weekly := rule("w", PayFlatPeriod, 70000, "2026-01-01", "")
	weekly.Unit = UnitWeek
	if got := earnings(t, Data{Catalog: Catalog{Employers: cat.Employers, PayRules: []PayRule{weekly}}}, "2026-03-02", "2026-03-15"); got.Total["EUR"] != 140000 {
		t.Fatalf("two weeks: %+v", got)
	}
}

func TestRuleAt(t *testing.T) {
	p := rule("p", PayHourly, 1, "2026-01-01", "")
	p.ProjectID = "x"
	rules := []PayRule{rule("a", PayHourly, 1, "2026-01-01", ""), rule("b", PayHourly, 1, "2026-06-01", ""), p}
	for _, c := range []struct {
		project, date, want string
	}{{"", "2026-03-01", "a"}, {"", "2026-07-01", "b"}, {"x", "2026-07-01", "p"}, {"y", "2026-07-01", "b"}} {
		got, ok := RuleAt(rules, c.project, d(c.date))
		if !ok || got.ID != c.want {
			t.Errorf("%s on %s: %s, want %s", c.project, c.date, got.ID, c.want)
		}
	}
	if _, ok := RuleAt(rules, "", d("2025-12-31")); ok {
		t.Error("a rule before its start")
	}
}
