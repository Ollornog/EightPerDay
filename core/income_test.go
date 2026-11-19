// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"testing"
)

// A tandem pilot: paid per flight with a customer and an amount, plus photos and tips, some in cash, some by card;
// the incomes add to what the pay rules compute and show by how they were taken.
func TestIncomeInEarnings(t *testing.T) {
	income := func(id, date string, kind IncomeKind, amount int64, by PaidBy) Income {
		return Income{ID: id, Owner: owner, EmployerID: "e1", Date: d(date), Kind: kind, Client: "a guest", Amount: amount,
			Currency: "EUR", PaidBy: by}
	}
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeExact, "")},
		PayRules: []PayRule{rule("h", PayHourly, 1000, "2026-01-01", "")},
		Incomes: []Income{income("i1", "2026-02-02", IncomeJob, 11000, PaidCard), income("i2", "2026-02-02", IncomeJob, 11000, PaidCash),
			income("i3", "2026-02-02", IncomePhotos, 3000, PaidCash), income("i4", "2026-02-03", IncomeTip, 1000, ""),
			income("i5", "2026-02-10", IncomeJob, 99999, PaidCash)}},
		Entries: []Entry{work("2026-02-02", 120)}}
	e := earnings(t, data, "2026-02-02", "2026-02-08")
	if e.Total["EUR"] != 2000+22000+3000+1000 {
		t.Fatalf("total %v", e.Total)
	}
	if e.Taken[PaidCash]["EUR"] != 14000 || e.Taken[PaidCard]["EUR"] != 11000 || len(e.Taken) != 2 {
		t.Errorf("taken %v", e.Taken)
	}
	reasons := map[string]int64{}
	for _, r := range e.Reasons {
		if r.Basis == PayIncome {
			reasons[r.Reason] = r.Amount
		}
	}
	if reasons["job"] != 22000 || reasons["photos"] != 3000 || reasons["tip"] != 1000 {
		t.Errorf("reasons %v", reasons)
	}
	for name, bad := range map[string]Income{
		"no amount":    income("x", "2026-02-02", IncomeJob, 0, PaidCash),
		"unknown kind": income("x", "2026-02-02", "bonus", 100, PaidCash),
		"unknown way":  income("x", "2026-02-02", IncomeTip, 100, "crypto"),
		"no day":       {ID: "x", Owner: owner, EmployerID: "e1", Kind: IncomeTip, Amount: 1, Currency: "EUR"},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDayInfo(t *testing.T) {
	ok := DayInfo{ID: DayInfoID("e1", d("2026-02-02")), Owner: owner, EmployerID: "e1", Date: d("2026-02-02"), Place: "Valley",
		KmOneWay: 125, KmTotal: 250, StandbyPassive: 480, StandbyActive: 60}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*DayInfo){
		"another day's ID": func(i *DayInfo) { i.ID = DayInfoID("e1", d("2026-02-03")) },
		"negative km":      func(i *DayInfo) { i.KmTotal = -1 },
		"standby over 24h": func(i *DayInfo) { i.StandbyPassive = 24 * 60 },
	} {
		bad := ok
		change(&bad)
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// 13th and 14th month pay of a monthly salary are spread over the months of the year.
func TestSalariesAYear(t *testing.T) {
	r := rule("m", PayFlatPeriod, 300000, "2026-01-01", "")
	r.Salaries = 14
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "")}, PayRules: []PayRule{r}}}
	if e := earnings(t, data, "2026-03-01", "2026-03-31"); e.Total["EUR"] != 350000 {
		t.Errorf("a month with 14 salaries: %v", e.Total)
	}
	r.Salaries = 0
	data.Catalog.PayRules = []PayRule{r}
	if e := earnings(t, data, "2026-03-01", "2026-03-31"); e.Total["EUR"] != 300000 {
		t.Errorf("a month of 12: %v", e.Total)
	}
	for _, bad := range []int{11, 16} {
		r.Salaries = bad
		if err := r.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%d salaries: %v", bad, err)
		}
	}
	r.Salaries, r.Unit = 13, UnitWeek
	if err := r.Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("salaries of a weekly rule: %v", err)
	}
}
