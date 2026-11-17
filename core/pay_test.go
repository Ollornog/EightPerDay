// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var berlin = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return loc
}()

// shift is a span from the local date and clock time (minutes after midnight) for the given minutes.
func shift(kind EntryKind, date string, at, minutes int) Entry {
	start := d(date).At(at, berlin)
	end := start.Add(time.Duration(minutes) * time.Minute)
	e := Entry{ID: NewID(), Owner: owner, EmployerID: "e1", Kind: kind, Start: &start, End: &end, Source: SourceManual}
	if err := e.Normalize(); err != nil {
		panic(err)
	}
	return e
}

func TestNightShiftSplit(t *testing.T) {
	cases := []struct {
		name  string
		split DaySplit
		entry Entry
		want  map[string]int // worked minutes per day
	}{
		{"start day keeps the whole night", "", shift(EntryWork, "2026-03-06", 22*60, 480),
			map[string]int{"2026-03-06": 480, "2026-03-07": 0}},
		{"split at midnight", SplitMidnight, shift(EntryWork, "2026-03-06", 22*60, 480),
			map[string]int{"2026-03-06": 120, "2026-03-07": 360}},
		// Autumn: the clocks go back at 03:00 on 25 October, so 22:00 to 06:00 is nine hours.
		{"autumn night is longer", SplitMidnight, shift(EntryWork, "2026-10-24", 22*60, 540),
			map[string]int{"2026-10-24": 120, "2026-10-25": 420}},
		// Spring: the clocks go forward at 02:00 on 29 March, so 22:00 to 06:00 is seven hours.
		{"spring night is shorter", SplitMidnight, shift(EntryWork, "2026-03-28", 22*60, 420),
			map[string]int{"2026-03-28": 120, "2026-03-29": 300}},
		{"a span within one day stays", SplitMidnight, shift(EntryWork, "2026-03-06", 8*60, 480),
			map[string]int{"2026-03-06": 480, "2026-03-07": 0}},
		// Seconds round per part (119.5 and 360.5 minutes), but the parts keep the sum of the entry.
		{"seconds keep the sum", SplitMidnight, func() Entry {
			e := shift(EntryWork, "2026-03-06", 22*60, 480)
			start, end := e.Start.Add(30*time.Second), e.End.Add(30*time.Second)
			e.Start, e.End = &start, &end
			_ = e.Normalize()
			return e
		}(), map[string]int{"2026-03-06": 120, "2026-03-07": 360}},
		{"a day entry has no clock", SplitMidnight, work("2026-03-06", 600),
			map[string]int{"2026-03-06": 600, "2026-03-07": 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			emp := employerOf(ModeExact, "")
			emp.DaySplit = c.split
			data := Data{Catalog: Catalog{Employers: []Employer{emp}}, Entries: []Entry{c.entry}}
			from, to := c.entry.Date, c.entry.Date.AddDays(1)
			days, err := Days(data, "e1", from, to)
			if err != nil {
				t.Fatal(err)
			}
			total := 0
			for date, want := range c.want {
				if got := dayOf(t, days, date).Worked; got != want {
					t.Errorf("%s: worked %d, want %d", date, got, want)
				}
				total += want
			}
			if total != c.entry.Minutes {
				t.Fatalf("the parts (%d) must add up to the entry (%d)", total, c.entry.Minutes)
			}
		})
	}

	// A break span across midnight is split as well, and the earnings follow the days.
	emp := employerOf(ModeExact, "")
	emp.DaySplit = SplitMidnight
	data := Data{Catalog: Catalog{Employers: []Employer{emp}, PayRules: []PayRule{
		rule("old", PayHourly, 2000, "2026-01-01", "2026-03-06"), rule("new", PayHourly, 3000, "2026-03-07", "")}},
		Entries: []Entry{shift(EntryWork, "2026-03-06", 22*60, 480), shift(EntryBreak, "2026-03-06", 23*60+45, 30)}}
	days := mustDays(t, data, "2026-03-06", "2026-03-07")
	if a, b := dayOf(t, days, "2026-03-06"), dayOf(t, days, "2026-03-07"); a.Worked != 105 || b.Worked != 345 ||
		a.Breaks != 15 || b.Breaks != 15 {
		t.Fatalf("break across midnight: %+v / %+v", a, b)
	}
	// 105 minutes at 20.00 on Friday, 345 at 30.00 from Saturday on.
	if got := earnings(t, data, "2026-03-06", "2026-03-07"); got.Total["EUR"] != 3500+17250 {
		t.Fatalf("earnings of a split night: %+v", got)
	}
	// Keeping the start day, a break after midnight is still taken off the night's day.
	emp.DaySplit = ""
	data.Entries = []Entry{shift(EntryWork, "2026-03-06", 20*60, 600), shift(EntryBreak, "2026-03-07", 0, 30),
		shift(EntryBreak, "2026-03-07", 8*60, 15)}
	data.Catalog.Employers = []Employer{emp}
	days = mustDays(t, data, "2026-03-06", "2026-03-07")
	if a, b := dayOf(t, days, "2026-03-06"), dayOf(t, days, "2026-03-07"); a.Worked != 570 || a.Breaks != 30 || b.Breaks != 15 {
		t.Fatalf("break after midnight, start day: %+v / %+v", a, b)
	}
	if err := (Employer{ID: "e", Owner: owner, Name: "x", Kind: KindEmployer, Mode: ModeExact, DaySplit: "noon"}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown day split accepted: %v", err)
	}
}

func surcharge(id string, kind SurchargeKind, bp int) Surcharge {
	return Surcharge{ID: id, Owner: owner, EmployerID: "e1", Kind: kind, ValidFrom: d("2026-01-01"), BasisPoints: bp}
}

func night(bp, from, to int) Surcharge {
	s := surcharge("night", SurchargeNight, bp)
	s.WindowStart, s.WindowEnd = from, to
	return s
}

// amountOf returns the amount of the lines with the basis and reason.
func amountOf(e Earnings, basis PayBasis, reason string) int64 {
	var n int64
	for _, r := range e.Reasons {
		if r.Basis == basis && r.Reason == reason {
			n += r.Amount
		}
	}
	return n
}

func TestSurcharges(t *testing.T) {
	hourly := rule("h", PayHourly, 2000, "2026-01-01", "") // 20.00 per hour: 1/3 cent per minute and percent
	cases := []struct {
		name       string
		region     string
		split      DaySplit
		surcharges []Surcharge
		entries    []Entry
		from, to   string
		want       map[SurchargeKind]int64
	}{
		{"night window over midnight", "", "", []Surcharge{night(2500, 20*60, 6*60)},
			[]Entry{shift(EntryWork, "2026-03-06", 18*60, 600)}, "2026-03-06", "2026-03-07",
			// 20:00 to 04:00 = 480 minutes at 25 % of 20.00.
			map[SurchargeKind]int64{SurchargeNight: 480 * 2000 * 2500 / 600000}},
		{"night window within a day", "", "", []Surcharge{night(2500, 23*60, 24*60)},
			[]Entry{shift(EntryWork, "2026-03-06", 22*60, 120)}, "2026-03-06", "2026-03-07",
			map[SurchargeKind]int64{SurchargeNight: 60 * 2000 * 2500 / 600000}},
		{"autumn night counts the extra hour", "", "",
			[]Surcharge{night(2500, 20*60, 6*60), surcharge("sun", SurchargeSunday, 5000)},
			[]Entry{shift(EntryWork, "2026-10-24", 22*60, 540)}, "2026-10-24", "2026-10-25",
			// 540 night minutes; Sunday from midnight: 420 minutes.
			map[SurchargeKind]int64{SurchargeNight: 540 * 2000 * 2500 / 600000, SurchargeSunday: 420 * 2000 * 5000 / 600000}},
		{"the Sunday part counts also when the day split keeps the start day", "", SplitStartDay,
			[]Surcharge{surcharge("sun", SurchargeSunday, 5000)},
			[]Entry{shift(EntryWork, "2026-03-07", 20*60, 360)}, "2026-03-07", "2026-03-07",
			map[SurchargeKind]int64{SurchargeSunday: 120 * 2000 * 5000 / 600000}},
		{"a timed break leaves the window", "", "", []Surcharge{night(2500, 20*60, 6*60)},
			[]Entry{shift(EntryWork, "2026-03-06", 20*60, 600), shift(EntryBreak, "2026-03-07", 0, 30)},
			"2026-03-06", "2026-03-07", map[SurchargeKind]int64{SurchargeNight: 570 * 2000 * 2500 / 600000}},
		{"an untimed break shrinks in proportion", "", "", []Surcharge{night(2500, 20*60, 6*60)},
			[]Entry{shift(EntryWork, "2026-03-06", 18*60, 600), withKind(work("2026-03-06", 60), EntryBreak)},
			"2026-03-06", "2026-03-07",
			// 480 night minutes of 600, less 60 untimed: 480 * 540 / 600 = 432.
			map[SurchargeKind]int64{SurchargeNight: 432 * 2000 * 2500 / 600000}},
		{"holiday", "DE-BY", "", []Surcharge{surcharge("hol", SurchargeHoliday, 12500)},
			[]Entry{work("2026-12-25", 480), work("2026-12-28", 480)}, "2026-12-25", "2026-12-31",
			map[SurchargeKind]int64{SurchargeHoliday: 480 * 2000 * 12500 / 600000}},
		{"a holiday on a Sunday pays the holiday surcharge alone", "DE-BY", "",
			[]Surcharge{surcharge("hol", SurchargeHoliday, 12500), surcharge("sun", SurchargeSunday, 5000)},
			[]Entry{work("2027-12-26", 240), work("2027-12-19", 60)}, "2027-12-13", "2027-12-26",
			map[SurchargeKind]int64{SurchargeHoliday: 240 * 2000 * 12500 / 600000, SurchargeSunday: 60 * 2000 * 5000 / 600000}},
		{"a Sunday holiday without holiday surcharge pays the Sunday one", "DE-BY", "",
			[]Surcharge{surcharge("sun", SurchargeSunday, 5000)},
			[]Entry{work("2027-12-26", 240)}, "2027-12-26", "2027-12-26",
			map[SurchargeKind]int64{SurchargeSunday: 240 * 2000 * 5000 / 600000}},
		{"night and holiday add up", "DE-BY", "",
			[]Surcharge{surcharge("hol", SurchargeHoliday, 12500), night(2500, 20*60, 6*60)},
			[]Entry{shift(EntryWork, "2026-12-24", 22*60, 240)}, "2026-12-24", "2026-12-25",
			map[SurchargeKind]int64{SurchargeHoliday: 120 * 2000 * 12500 / 600000, SurchargeNight: 240 * 2000 * 2500 / 600000}},
		{"before the rule is valid", "", "", []Surcharge{func() Surcharge {
			s := night(2500, 20*60, 6*60)
			s.ValidFrom = d("2026-04-01")
			return s
		}()}, []Entry{shift(EntryWork, "2026-03-06", 18*60, 600)}, "2026-03-06", "2026-03-07",
			map[SurchargeKind]int64{SurchargeNight: 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			emp := employerOf(ModeExact, c.region)
			emp.DaySplit = c.split
			data := Data{Catalog: Catalog{Employers: []Employer{emp}, PayRules: []PayRule{hourly},
				Surcharges: c.surcharges}, Entries: c.entries}
			got := earnings(t, data, c.from, c.to)
			for kind, want := range c.want {
				if amount := amountOf(got, PaySurcharge, string(kind)); amount != want {
					t.Errorf("%s: %d, want %d (%+v)", kind, amount, want, got.Lines)
				}
			}
		})
	}
}

func TestOvertimeSurchargeAndBase(t *testing.T) {
	sched := week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)
	var entries []Entry
	for _, date := range []string{"2026-03-02", "2026-03-03", "2026-03-04", "2026-03-05", "2026-03-06"} {
		entries = append(entries, work(date, 540))
	}
	entries = append(entries, work("2026-03-09", 300)) // a short Monday does not eat into last week's overtime
	over := surcharge("ot", SurchargeOvertime, 2500)
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "")}, Schedules: []Schedule{sched},
		PayRules: []PayRule{rule("h", PayHourly, 2000, "2026-01-01", "")}, Surcharges: []Surcharge{over}}, Entries: entries}
	got := earnings(t, data, "2026-03-02", "2026-03-15")
	if want := int64(300 * 2000 * 2500 / 600000); amountOf(got, PaySurcharge, "overtime") != want {
		t.Fatalf("overtime surcharge: %+v, want %d", got.Reasons, want)
	}
	// A salaried employer names the hourly base.
	salaried := over
	salaried.BaseAmount, salaried.Currency = 3000, "EUR"
	data.Catalog.PayRules = []PayRule{rule("m", PayFlatPeriod, 300000, "2026-01-01", "")}
	data.Catalog.Surcharges = []Surcharge{salaried}
	got = earnings(t, data, "2026-03-02", "2026-03-15")
	if want := int64(300 * 3000 * 2500 / 600000); amountOf(got, PaySurcharge, "overtime") != want {
		t.Fatalf("overtime surcharge on a base: %+v, want %d", got.Reasons, want)
	}
	// Without a base and without an hourly rule nothing can be paid.
	data.Catalog.Surcharges = []Surcharge{over}
	if got := earnings(t, data, "2026-03-02", "2026-03-15"); amountOf(got, PaySurcharge, "overtime") != 0 {
		t.Fatalf("surcharge without a base: %+v", got.Reasons)
	}
}

func absencePay(id, typ string, method AbsencePayMethod) AbsencePay {
	return AbsencePay{ID: id, Owner: owner, EmployerID: "e1", Type: typ, Method: method, ValidFrom: d("2026-01-01")}
}

func TestPaidAbsences(t *testing.T) {
	sched := week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)
	hourly := rule("h", PayHourly, 2000, "2026-01-01", "")
	half := absence(AbsenceVacation, "2026-05-06", "2026-05-06")
	half.HalfDay = true
	cases := []struct {
		name     string
		pays     []AbsencePay
		absences []Absence
		want     map[string]int64
	}{
		{"leave at the target", []AbsencePay{absencePay("v", AbsenceVacation, PayTarget)},
			[]Absence{absence(AbsenceVacation, "2026-05-04", "2026-05-05")},
			map[string]int64{AbsenceVacation: 2 * 480 * 2000 / 60}},
		{"half a day", []AbsencePay{absencePay("v", AbsenceVacation, PayTarget)}, []Absence{half},
			map[string]int64{AbsenceVacation: 240 * 2000 / 60}},
		{"an absence type without a rule is unpaid", []AbsencePay{absencePay("v", AbsenceVacation, PayTarget)},
			[]Absence{absence(AbsenceSick, "2026-05-04", "2026-05-04")}, map[string]int64{AbsenceSick: 0}},
		{"public holiday", []AbsencePay{absencePay("ph", PublicHolidayPay, PayTarget)}, nil,
			// 1 May, Ascension Day (14 May) and Whit Monday (25 May) fall on working days in May 2026.
			map[string]int64{PublicHolidayPay: 3 * 480 * 2000 / 60}},
		{"leave over a public holiday pays the holiday once", []AbsencePay{absencePay("ph", PublicHolidayPay, PayTarget),
			absencePay("v", AbsenceVacation, PayTarget)}, []Absence{absence(AbsenceVacation, "2026-05-01", "2026-05-04")},
			// 1 May is a holiday, the weekend has no target: one day of leave.
			map[string]int64{PublicHolidayPay: 3 * 480 * 2000 / 60, AbsenceVacation: 480 * 2000 / 60}},
		{"sickness at the target", []AbsencePay{absencePay("s", AbsenceSick, PayTarget)},
			[]Absence{absence(AbsenceSick, "2026-05-08", "2026-05-11")},
			// Friday and Monday; the weekend has no target.
			map[string]int64{AbsenceSick: 2 * 480 * 2000 / 60}},
		{"a rule that ended", []AbsencePay{func() AbsencePay {
			p := absencePay("v", AbsenceVacation, PayTarget)
			p.ValidTo = d("2026-04-30")
			return p
		}()}, []Absence{absence(AbsenceVacation, "2026-05-04", "2026-05-04")}, map[string]int64{AbsenceVacation: 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")},
				Schedules: []Schedule{sched}, PayRules: []PayRule{hourly}, AbsencePays: c.pays}, Absences: c.absences}
			got := earnings(t, data, "2026-05-01", "2026-05-31")
			for reason, want := range c.want {
				if amount := amountOf(got, PayAbsence, reason); amount != want {
					t.Errorf("%s: %d, want %d (%+v)", reason, amount, want, got.Lines)
				}
			}
		})
	}
}

func TestPaidAbsenceAverage(t *testing.T) {
	// Reference: the week before the leave, three days at 20.00 and two at 30.00 an hour (the snapshots), 480 minutes
	// each: 960.00 for 2400 minutes, 24.00 an hour on average.
	var entries []Entry
	for i, date := range []string{"2026-04-27", "2026-04-28", "2026-04-29", "2026-04-30", "2026-05-01"} {
		e := work(date, 480)
		e.Rate = &RateSnapshot{RuleID: "h", Amount: 2000, Currency: "EUR"}
		if i >= 3 {
			e.Rate.Amount = 3000
		}
		entries = append(entries, e)
	}
	pay := absencePay("v", AbsenceVacation, PayAverage)
	pay.Weeks = 1
	leave := absence(AbsenceVacation, "2026-05-04", "2026-05-05")
	sched := week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)
	for _, c := range []struct {
		name      string
		schedules []Schedule
		mode      Mode
	}{
		{"at the target", []Schedule{sched}, ModeSchedule},
		{"without a target: the average worked day", nil, ModeExact},
	} {
		t.Run(c.name, func(t *testing.T) {
			data := Data{Catalog: Catalog{Employers: []Employer{employerOf(c.mode, "")}, Schedules: c.schedules,
				PayRules: []PayRule{rule("h", PayHourly, 2000, "2026-01-01", "")}, AbsencePays: []AbsencePay{pay}},
				Entries: entries, Absences: []Absence{leave}}
			got := earnings(t, data, "2026-05-04", "2026-05-08")
			if want := int64(2 * 480 * 96000 / 2400); amountOf(got, PayAbsence, AbsenceVacation) != want {
				t.Fatalf("average leave pay %+v, want %d", got.Lines, want)
			}
			for _, l := range got.Lines {
				if l.Basis == PayAbsence && (l.Rate != 2400 || l.Quantity != 960) {
					t.Fatalf("line: %+v", l)
				}
			}
		})
	}
	// Without work in the reference there is no average to pay.
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "")}, Schedules: []Schedule{sched},
		AbsencePays: []AbsencePay{pay}}, Absences: []Absence{leave}}
	if got := earnings(t, data, "2026-05-04", "2026-05-08"); len(got.Lines) != 0 {
		t.Fatalf("average without a reference: %+v", got.Lines)
	}
	if got := EarningsDataFrom(Catalog{AbsencePays: []AbsencePay{pay}}, "e1", d("2026-05-04")); got != d("2026-05-04").AddDays(-367-7) {
		t.Fatalf("data from %s", got)
	}
	if got := EarningsDataFrom(Catalog{}, "e1", d("2026-05-04")); got != d("2026-05-03") {
		t.Fatalf("data from %s", got)
	}
}

func TestEarningsByReason(t *testing.T) {
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")},
		Schedules: []Schedule{week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)},
		PayRules:  []PayRule{rule("h", PayHourly, 2000, "2026-01-01", "")},
		AbsencePays: []AbsencePay{absencePay("ph", PublicHolidayPay, PayTarget),
			absencePay("v", AbsenceVacation, PayTarget)},
		Surcharges: []Surcharge{night(2500, 20*60, 6*60)}},
		Entries:  []Entry{shift(EntryWork, "2026-05-04", 18*60, 480)},
		Absences: []Absence{absence(AbsenceVacation, "2026-05-05", "2026-05-05")}}
	got := earnings(t, data, "2026-05-01", "2026-05-05")
	want := []ReasonTotal{
		{PayHourly, ReasonWork, "EUR", 16000},
		{PayAbsence, PublicHolidayPay, "EUR", 16000},
		{PayAbsence, AbsenceVacation, "EUR", 16000},
		{PaySurcharge, string(SurchargeNight), "EUR", 6 * 60 * 2000 * 2500 / 600000},
	}
	if len(got.Reasons) != len(want) {
		t.Fatalf("reasons: %+v", got.Reasons)
	}
	var sum int64
	for i, r := range want {
		if got.Reasons[i] != r {
			t.Errorf("reason %d: %+v, want %+v", i, got.Reasons[i], r)
		}
		sum += r.Amount
	}
	if got.Total["EUR"] != sum {
		t.Fatalf("total %d, reasons %d", got.Total["EUR"], sum)
	}
}

func TestPayRuleValidation(t *testing.T) {
	good := []Record{absencePay("a", AbsenceVacation, PayAverage), surcharge("s", SurchargeSunday, 5000),
		night(2500, 22*60, 6*60), func() Surcharge {
			s := surcharge("b", SurchargeOvertime, 2500)
			s.BaseAmount, s.Currency = 2000, "EUR"
			return s
		}()}
	for _, r := range good {
		if err := r.Validate(); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	bad := []Record{
		absencePay("a", "bad key", PayTarget),
		absencePay("a", AbsenceVacation, "guess"),
		func() AbsencePay { p := absencePay("a", AbsenceVacation, PayAverage); p.Weeks = -1; return p }(),
		func() AbsencePay {
			p := absencePay("a", AbsenceVacation, PayTarget)
			p.ValidTo = d("2025-12-31")
			return p
		}(),
		surcharge("s", "weekend", 5000),
		surcharge("s", SurchargeSunday, 0),
		night(2500, 6*60, 6*60),
		night(2500, -1, 6*60),
		func() Surcharge { s := surcharge("s", SurchargeSunday, 5000); s.WindowEnd = 60; return s }(),
		func() Surcharge { s := surcharge("s", SurchargeSunday, 5000); s.BaseAmount = 100; return s }(),
	}
	for _, r := range bad {
		if err := r.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted", r)
		}
	}
	if (PayRule{ID: "p", Owner: owner, EmployerID: "e1", ValidFrom: d("2026-01-01"), Basis: PaySurcharge, Currency: "EUR"}).Validate() == nil {
		t.Error("a pay rule with a line basis")
	}
}

// Every catalog kind goes through decoding, the catalog, the export and the import alike.
func TestEveryKindRoundTrips(t *testing.T) {
	samples := map[Kind]Record{
		RecEmployer:     employerOf(ModeExact, ""),
		RecModeChange:   ModeChange{ID: "x", Owner: owner, EmployerID: "e1", ValidFrom: d("2026-01-01"), Mode: ModeExact},
		RecSchedule:     week("x", "2026-01-01", 480),
		RecPayRule:      rule("x", PayHourly, 1, "2026-01-01", ""),
		RecClient:       Client{ID: "x", Owner: owner, Name: "c"},
		RecProject:      Project{ID: "x", Owner: owner, Name: "p"},
		RecActivity:     Activity{ID: "x", Owner: owner, Name: "a"},
		RecTag:          Tag{ID: "x", Owner: owner, Path: "t"},
		RecAbsenceType:  AbsenceType{ID: "x", Owner: owner, Key: "k", Name: "n", Effect: InfoOnly},
		RecConfirmation: Confirmation{ID: "x", Owner: owner, EmployerID: "e1", From: d("2026-01-01"), To: d("2026-01-02")},
		RecAdjustment:   Adjustment{ID: "x", Owner: owner, EmployerID: "e1", Date: d("2026-01-01"), Kind: AdjustLeave, Reason: "r"},
		RecPeriodClose:  PeriodClose{ID: "x", Owner: owner, EmployerID: "e1", From: d("2026-01-01"), To: d("2026-01-02")},
		RecHoliday:      Holiday{ID: "x", Owner: owner, Date: d("2026-01-01"), Name: "h", Fraction: 1000},
		RecLeaveNotice:  LeaveNotice{ID: "x", Owner: owner, EmployerID: "e1", Year: 2026, NotifiedAt: time.Now()},
		RecPurgeLog:     PurgeLog{ID: "x", Owner: owner},
		RecAbsencePay:   absencePay("x", AbsenceVacation, PayTarget),
		RecSurcharge:    surcharge("x", SurchargeSunday, 5000),
	}
	if len(samples) != len(Kinds) {
		t.Fatalf("%d samples for %d kinds", len(samples), len(Kinds))
	}
	for _, kind := range Kinds {
		r, ok := samples[kind]
		if !ok || r.Meta().Kind != kind {
			t.Fatalf("no sample of %s", kind)
		}
		if err := r.Validate(); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		back, err := DecodeRecord(kind, data)
		if err != nil || back.Meta() != r.Meta() {
			t.Fatalf("%s decodes to %+v %v", kind, back, err)
		}
		var c Catalog
		c.Add(back)
		recs := catalogRecords(c)
		if kind == RecPurgeLog {
			// Purge logs stay with their store: no export, no import.
			if len(recs) != 0 || len(c.PurgeLogs) != 1 {
				t.Fatalf("purge log: %+v", c)
			}
			continue
		}
		if len(recs) != 1 || withOwner(recs[0], "bob").Meta().Owner != "bob" {
			t.Fatalf("%s is lost on the way through the catalog, export or import", kind)
		}
	}
}
