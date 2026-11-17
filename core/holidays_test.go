// SPDX-License-Identifier: MIT

package core

import (
	"slices"
	"testing"
	"time"
)

func TestEasterSunday(t *testing.T) {
	for year, want := range map[int]string{2019: "2019-04-21", 2024: "2024-03-31", 2025: "2025-04-20",
		2026: "2026-04-05", 2027: "2027-03-28", 2038: "2038-04-25", 2285: "2285-03-22"} {
		if got := EasterSunday(year); got != d(want) {
			t.Errorf("Easter %d = %s, want %s", year, got, want)
		}
	}
}

func names(hs []PublicHoliday) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Date.String()+" "+h.Name)
	}
	return out
}

func TestPublicHolidays(t *testing.T) {
	cases := []struct {
		region string
		year   int
		count  int
		has    []string
		hasNot []string
	}{
		{"DE-BY", 2026, 13, []string{"2026-01-06 Epiphany", "2026-06-04 Corpus Christi", "2026-08-15 Assumption Day",
			"2026-11-01 All Saints' Day", "2026-05-14 Ascension Day", "2026-05-25 Whit Monday"},
			[]string{"2026-10-31 Reformation Day"}},
		{"DE-BE", 2026, 10, []string{"2026-03-08 International Women's Day"}, []string{"2026-05-08 Liberation Day"}},
		{"DE-BE", 2025, 11, []string{"2025-05-08 Liberation Day"}, nil},
		{"DE-BE", 2018, 9, nil, []string{"2018-03-08 International Women's Day"}},
		{"DE-MV", 2023, 11, []string{"2023-03-08 International Women's Day", "2023-10-31 Reformation Day"}, nil},
		{"DE-MV", 2022, 10, nil, []string{"2022-03-08 International Women's Day"}},
		{"DE-SN", 2026, 11, []string{"2026-11-18 Repentance and Prayer Day"}, nil},
		{"DE-SN", 2025, 11, []string{"2025-11-19 Repentance and Prayer Day"}, nil},
		{"DE-TH", 2026, 11, []string{"2026-09-20 World Children's Day"}, nil},
		{"DE-BB", 2026, 12, []string{"2026-04-05 Easter Sunday", "2026-05-24 Whit Sunday"}, nil},
		{"DE-NI", 2017, 10, []string{"2017-10-31 Reformation Day"}, nil},
		{"DE-NI", 2016, 9, nil, []string{"2016-10-31 Reformation Day"}},
		{"DE-NW", 2017, 12, []string{"2017-10-31 Reformation Day"}, nil},
		{"DE-NW", 2018, 11, nil, []string{"2018-10-31 Reformation Day"}},
		{"DE", 2026, 9, []string{"2026-04-03 Good Friday", "2026-10-03 Day of German Unity"}, []string{"2026-01-06 Epiphany"}},
		{"AT", 2026, 13, []string{"2026-12-08 Immaculate Conception", "2026-10-26 National Day"},
			[]string{"2026-04-03 Good Friday"}},
	}
	for _, c := range cases {
		got := names(PublicHolidays(c.region, c.year))
		if len(got) != c.count {
			t.Errorf("%s %d: %d holidays, want %d: %v", c.region, c.year, len(got), c.count, got)
		}
		for _, h := range c.has {
			if !slices.Contains(got, h) {
				t.Errorf("%s %d: missing %s", c.region, c.year, h)
			}
		}
		for _, h := range c.hasNot {
			if slices.Contains(got, h) {
				t.Errorf("%s %d: unexpected %s", c.region, c.year, h)
			}
		}
		if !slices.IsSortedFunc(PublicHolidays(c.region, c.year), func(a, b PublicHoliday) int { return a.Date.Compare(b.Date) }) {
			t.Errorf("%s %d: not sorted", c.region, c.year)
		}
	}
}

func TestRegions(t *testing.T) {
	if len(Regions()) != 22 || !KnownRegion("DE-HH") || !KnownRegion("AT") || KnownRegion("DE-XX") {
		t.Fatalf("regions: %v", Regions())
	}
	AddHolidayRule(HolidayRule{Name: "Test Day", Month: time.July, Day: 4, Regions: []string{"ZZ-TEST"}})
	if !KnownRegion("ZZ-TEST") || len(PublicHolidays("ZZ-TEST", 2026)) != 1 {
		t.Fatal("added rule not found")
	}
}

func TestOwnHolidays(t *testing.T) {
	data := Data{Catalog: Catalog{
		Employers: []Employer{employerOf(ModeSchedule, "DE-BY")},
		Schedules: []Schedule{week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)},
		Holidays: []Holiday{
			{ID: "h1", Owner: owner, Date: d("2026-12-24"), Name: "Christmas Eve", Fraction: 500},
			{ID: "h2", Owner: owner, EmployerID: "other", Date: d("2026-12-31"), Name: "Not ours", Fraction: 1000},
			{ID: "h3", Owner: owner, EmployerID: "e1", Date: d("2026-08-08"), Name: "Peace Festival", Fraction: 1000},
		},
	}}
	days := mustDays(t, data, "2026-08-07", "2026-12-31")
	for _, c := range []struct {
		date     string
		holiday  string
		adjusted int
	}{
		{"2026-12-24", "Christmas Eve", 240},
		{"2026-12-31", "", 480},
		{"2026-12-25", "Christmas Day", 0},
		{"2026-08-08", "Peace Festival", 0}, // a Saturday: no target anyway
		{"2026-08-07", "", 480},
	} {
		got := dayOf(t, days, c.date)
		if got.Holiday != c.holiday || got.AdjustedTarget != c.adjusted {
			t.Errorf("%s: holiday %q adjusted %d, want %q %d", c.date, got.Holiday, got.AdjustedTarget, c.holiday, c.adjusted)
		}
	}
}

// Parts of a state have every holiday of the state, plus or minus the days their municipalities keep.
func TestRegionalHolidays(t *testing.T) {
	cases := []struct {
		region string
		year   int
		count  int
		has    []string
		hasNot []string
	}{
		{"DE-BY-AUGSBURG", 2026, 14, []string{"2026-08-08 Augsburg Peace Festival", "2026-08-15 Assumption Day",
			"2026-06-04 Corpus Christi", "2026-01-01 New Year's Day"}, nil},
		{"DE-BY-PROTESTANT", 2026, 12, []string{"2026-06-04 Corpus Christi", "2026-01-06 Epiphany"},
			[]string{"2026-08-15 Assumption Day", "2026-08-08 Augsburg Peace Festival"}},
		{"DE-BY", 2026, 13, []string{"2026-08-15 Assumption Day"}, []string{"2026-08-08 Augsburg Peace Festival"}},
		{"DE-SN-CATHOLIC", 2026, 12, []string{"2026-06-04 Corpus Christi", "2026-11-18 Repentance and Prayer Day"}, nil},
		{"DE-SN", 2026, 11, nil, []string{"2026-06-04 Corpus Christi"}},
		{"DE-TH-CATHOLIC", 2026, 12, []string{"2026-06-04 Corpus Christi", "2026-09-20 World Children's Day"}, nil},
		{"DE-TH", 2026, 11, nil, []string{"2026-06-04 Corpus Christi"}},
		// A code that merely starts like a state is no part of it: only the days of every German state.
		{"DE-BYX", 2026, 9, nil, []string{"2026-01-06 Epiphany", "2026-08-15 Assumption Day"}},
	}
	for _, c := range cases {
		got := names(PublicHolidays(c.region, c.year))
		if len(got) != c.count {
			t.Errorf("%s %d: %d holidays, want %d: %v", c.region, c.year, len(got), c.count, got)
		}
		for _, h := range c.has {
			if !slices.Contains(got, h) {
				t.Errorf("%s %d: missing %s", c.region, c.year, h)
			}
		}
		for _, h := range c.hasNot {
			if slices.Contains(got, h) {
				t.Errorf("%s %d: unexpected %s", c.region, c.year, h)
			}
		}
	}
	// Every municipal day names its source.
	for _, region := range []string{"DE-BY-AUGSBURG", "DE-SN-CATHOLIC", "DE-TH-CATHOLIC"} {
		for _, h := range PublicHolidays(region, 2026) {
			if (h.Name == "Augsburg Peace Festival" || (h.Name == "Corpus Christi" && region != "DE-BY-AUGSBURG")) && h.Source == "" {
				t.Errorf("%s: %s without source", region, h.Name)
			}
		}
	}
	for _, code := range []string{"DE-BY-AUGSBURG", "DE-BY-PROTESTANT", "DE-SN-CATHOLIC", "DE-TH-CATHOLIC"} {
		if !KnownRegion(code) {
			t.Errorf("%s unknown", code)
		}
	}
	emp := employerOf(ModeSchedule, "DE-BY-AUGSBURG")
	if err := emp.Validate(); err != nil {
		t.Fatal(err)
	}
	data := Data{Catalog: Catalog{Employers: []Employer{emp}, Schedules: []Schedule{week("s", "2026-01-01", 480, 480, 480, 480, 480, 480, 0)}}}
	if got := dayOf(t, mustDays(t, data, "2026-08-08", "2026-08-08"), "2026-08-08"); got.Holiday != "Augsburg Peace Festival" || got.AdjustedTarget != 0 {
		t.Fatalf("peace festival in Augsburg: %+v", got)
	}
}
