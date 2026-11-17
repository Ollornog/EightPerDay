// SPDX-License-Identifier: MIT

package core

import (
	"slices"
	"strings"
	"sync"
	"time"
)

// HolidayRule is one public holiday as data: either a fixed day of the year (Month, Day), an offset from Easter Sunday
// (Easter), or the Wednesday before 23 November (BussUndBettag). It applies in the listed regions from FromYear to
// ToYear (0 means unbounded). A region "DE" means every German state.
type HolidayRule struct {
	Name          string
	Month         time.Month
	Day           int
	Easter        *int
	BussUndBettag bool
	Regions       []string
	FromYear      int
	ToYear        int
}

// PublicHoliday is a public holiday on a date.
type PublicHoliday struct {
	Date Date
	Name string
}

func easter(offset int) *int { return &offset }

var (
	rulesMu sync.RWMutex
	// The German states (ISO 3166-2:DE) and Austria. Sources: the holiday laws of the states and the Austrian
	// Arbeitsruhegesetz; Sundays that are holidays by law (Easter and Whit Sunday in Brandenburg) are included.
	// Regional days that only apply in some municipalities (Assumption in parts of Bavaria, Corpus Christi in parts of
	// Saxony and Thuringia, the Augsburg peace festival) are left to own holidays (Holiday records); Bavaria counts
	// Assumption here, as most Bavarian municipalities are Catholic.
	holidayRules = []HolidayRule{
		{Name: "New Year's Day", Month: time.January, Day: 1, Regions: []string{"DE", "AT"}},
		{Name: "Epiphany", Month: time.January, Day: 6, Regions: []string{"DE-BW", "DE-BY", "DE-ST", "AT"}},
		{Name: "International Women's Day", Month: time.March, Day: 8, Regions: []string{"DE-BE"}, FromYear: 2019},
		{Name: "International Women's Day", Month: time.March, Day: 8, Regions: []string{"DE-MV"}, FromYear: 2023},
		{Name: "Good Friday", Easter: easter(-2), Regions: []string{"DE"}},
		{Name: "Easter Sunday", Easter: easter(0), Regions: []string{"DE-BB"}},
		{Name: "Easter Monday", Easter: easter(1), Regions: []string{"DE", "AT"}},
		{Name: "Labour Day", Month: time.May, Day: 1, Regions: []string{"DE", "AT"}},
		{Name: "Liberation Day", Month: time.May, Day: 8, Regions: []string{"DE-BE"}, FromYear: 2020, ToYear: 2020},
		{Name: "Liberation Day", Month: time.May, Day: 8, Regions: []string{"DE-BE"}, FromYear: 2025, ToYear: 2025},
		{Name: "Ascension Day", Easter: easter(39), Regions: []string{"DE", "AT"}},
		{Name: "Whit Sunday", Easter: easter(49), Regions: []string{"DE-BB"}},
		{Name: "Whit Monday", Easter: easter(50), Regions: []string{"DE", "AT"}},
		{Name: "Corpus Christi", Easter: easter(60), Regions: []string{"DE-BW", "DE-BY", "DE-HE", "DE-NW", "DE-RP", "DE-SL", "AT"}},
		{Name: "Assumption Day", Month: time.August, Day: 15, Regions: []string{"DE-BY", "DE-SL", "AT"}},
		{Name: "World Children's Day", Month: time.September, Day: 20, Regions: []string{"DE-TH"}, FromYear: 2019},
		{Name: "Day of German Unity", Month: time.October, Day: 3, Regions: []string{"DE"}, FromYear: 1990},
		{Name: "National Day", Month: time.October, Day: 26, Regions: []string{"AT"}},
		{Name: "Reformation Day", Month: time.October, Day: 31, Regions: []string{"DE-BB", "DE-MV", "DE-SN", "DE-ST", "DE-TH"}},
		{Name: "Reformation Day", Month: time.October, Day: 31, Regions: []string{"DE-HB", "DE-HH", "DE-NI", "DE-SH"}, FromYear: 2018},
		{Name: "Reformation Day", Month: time.October, Day: 31, Regions: []string{"DE-BE", "DE-BW", "DE-BY", "DE-HE", "DE-NW", "DE-RP", "DE-SL"}, FromYear: 2017, ToYear: 2017},
		{Name: "Reformation Day", Month: time.October, Day: 31, Regions: []string{"DE-HB", "DE-HH", "DE-NI", "DE-SH"}, FromYear: 2017, ToYear: 2017},
		{Name: "All Saints' Day", Month: time.November, Day: 1, Regions: []string{"DE-BW", "DE-BY", "DE-NW", "DE-RP", "DE-SL", "AT"}},
		{Name: "Repentance and Prayer Day", BussUndBettag: true, Regions: []string{"DE-SN"}},
		{Name: "Immaculate Conception", Month: time.December, Day: 8, Regions: []string{"AT"}},
		{Name: "Christmas Day", Month: time.December, Day: 25, Regions: []string{"DE", "AT"}},
		{Name: "St. Stephen's Day", Month: time.December, Day: 26, Regions: []string{"DE", "AT"}},
	}
	regions = []string{"DE", "DE-BB", "DE-BE", "DE-BW", "DE-BY", "DE-HB", "DE-HE", "DE-HH", "DE-MV", "DE-NI", "DE-NW",
		"DE-RP", "DE-SH", "DE-SL", "DE-SN", "DE-ST", "DE-TH", "AT"}
)

// Regions lists the region codes with public holidays.
func Regions() []string {
	rulesMu.RLock()
	defer rulesMu.RUnlock()
	return slices.Clone(regions)
}

// KnownRegion reports whether code is a region with public holidays.
func KnownRegion(code string) bool {
	rulesMu.RLock()
	defer rulesMu.RUnlock()
	return slices.Contains(regions, code)
}

// AddHolidayRule extends the data with a rule, for example for another country. Regions it names become known.
func AddHolidayRule(r HolidayRule) {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	holidayRules = append(holidayRules, r)
	for _, code := range r.Regions {
		if !slices.Contains(regions, code) {
			regions = append(regions, code)
		}
	}
}

// EasterSunday returns Easter Sunday of the Gregorian calendar (anonymous Gregorian algorithm).
func EasterSunday(year int) Date {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return NewDate(year, time.Month(month), day)
}

func ruleApplies(rule HolidayRule, region string, year int) bool {
	if (rule.FromYear != 0 && year < rule.FromYear) || (rule.ToYear != 0 && year > rule.ToYear) {
		return false
	}
	for _, r := range rule.Regions {
		if r == region || (r == "DE" && strings.HasPrefix(region, "DE-")) {
			return true
		}
	}
	return false
}

// PublicHolidays returns the public holidays of the region in the year, ordered by date. A region "DE" alone returns
// only the holidays of every German state.
func PublicHolidays(region string, year int) []PublicHoliday {
	rulesMu.RLock()
	defer rulesMu.RUnlock()
	var out []PublicHoliday
	for _, rule := range holidayRules {
		if !ruleApplies(rule, region, year) {
			continue
		}
		var d Date
		switch {
		case rule.Easter != nil:
			d = EasterSunday(year).AddDays(*rule.Easter)
		case rule.BussUndBettag:
			nov22 := NewDate(year, time.November, 22)
			d = nov22.AddDays(-((int(nov22.Weekday()) - int(time.Wednesday) + 7) % 7))
		default:
			d = NewDate(year, rule.Month, rule.Day)
		}
		out = append(out, PublicHoliday{Date: d, Name: rule.Name})
	}
	slices.SortFunc(out, func(a, b PublicHoliday) int { return a.Date.Compare(b.Date) })
	return out
}

// holidayCalendar answers "is this a day off, and how much of it" for one employer: the public holidays of its region
// plus its own holidays and those of the owner.
type holidayCalendar struct {
	region string
	own    []Holiday
	years  map[int]map[Date]string
}

func newHolidayCalendar(cat Catalog, emp Employer) *holidayCalendar {
	h := &holidayCalendar{region: emp.Region, years: map[int]map[Date]string{}}
	for _, x := range cat.Holidays {
		if appliesTo(emp, x.EmployerID) {
			h.own = append(h.own, x)
		}
	}
	return h
}

// on returns the name of the day off and its fraction in thousandths (0 if it is none).
func (h *holidayCalendar) on(d Date) (string, int) {
	name, fraction := "", 0
	if h.region != "" {
		days, ok := h.years[d.Year]
		if !ok {
			days = map[Date]string{}
			for _, p := range PublicHolidays(h.region, d.Year) {
				days[p.Date] = p.Name
			}
			h.years[d.Year] = days
		}
		if n, ok := days[d]; ok {
			name, fraction = n, 1000
		}
	}
	for _, x := range h.own {
		if x.Date == d && x.Fraction > fraction {
			name, fraction = x.Name, x.Fraction
		}
	}
	return name, fraction
}
