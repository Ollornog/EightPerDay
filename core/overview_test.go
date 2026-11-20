// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func managedEmployer(id, org string) Employer {
	e := employerOf(ModeSchedule, "")
	e.ID, e.Holder, e.Organization = id, HolderManaged, org
	return e
}

// A managed employer counts only what is booked for it: absences, holidays and absence types for every employer stay
// private, so the person and the organization see the same numbers.
func TestManagedEmployerIgnoresPrivateRecords(t *testing.T) {
	m := managedEmployer("m1", "acme")
	p := employerOf(ModeSchedule, "")
	p.ID = "p1"
	sched := func(id, emp string) Schedule {
		s := week(id, "2026-01-01", 480, 480, 480, 480, 480, 0, 0)
		s.EmployerID = emp
		return s
	}
	cat := Catalog{Employers: []Employer{m, p}, Schedules: []Schedule{sched("a", "m1"), sched("b", "p1")},
		Holidays:     []Holiday{{ID: "h", Owner: owner, Date: d("2026-03-03"), Name: "Own", Fraction: 1000}},
		AbsenceTypes: []AbsenceType{{ID: "t", Owner: owner, Key: AbsenceSick, Name: "Sick", Effect: FillsTarget}}}
	sick := absence(AbsenceSick, "2026-03-02", "2026-03-02")
	sick.EmployerID = ""
	data := Data{Catalog: cat, Absences: []Absence{sick}}

	for _, c := range []struct {
		emp                  string
		monday, tuesday      int // adjusted target
		mondayCredit         int
		mondayAbsences       int
		tuesdayHoliday       string
		typesWithFillsTarget int
	}{
		{"p1", 480, 0, 480, 1, "Own", 1},
		{"m1", 480, 480, 0, 0, "", 0},
	} {
		days, err := Days(data, c.emp, d("2026-03-02"), d("2026-03-03"))
		if err != nil {
			t.Fatal(err)
		}
		if days[0].AdjustedTarget != c.monday || days[0].Credited != c.mondayCredit || len(days[0].Absences) != c.mondayAbsences ||
			days[1].AdjustedTarget != c.tuesday || days[1].Holiday != c.tuesdayHoliday {
			t.Errorf("%s: %+v / %+v", c.emp, days[0], days[1])
		}
		n := 0
		for _, ty := range cat.AbsenceTypesFor(c.emp) {
			if ty.Effect == FillsTarget {
				n++
			}
		}
		if n != c.typesWithFillsTarget {
			t.Errorf("%s: %d types fill the target", c.emp, n)
		}
	}
}

func TestPermissions(t *testing.T) {
	p := employerOf(ModeSchedule, "")
	m := managedEmployer("m1", "acme")
	alice := Actor{Person: owner}
	admin := Actor{Person: "boss", Organization: "acme", Admin: true}
	other := Actor{Person: "boss", Organization: "globex", Admin: true}
	member := Actor{Person: "colleague", Organization: "acme"}
	cases := []struct {
		name               string
		emp                Employer
		actor              Actor
		settings, book, rm bool
	}{
		{"owner, private", p, alice, true, true, true},
		{"owner, managed", m, alice, false, true, false},
		{"admin, managed", m, admin, true, false, false},
		{"admin of another organization", m, other, false, false, false},
		{"member without admin rights", m, member, false, false, false},
		{"admin, someone's private employer", p, admin, false, false, false},
		{"nobody", p, Actor{}, false, false, false},
	}
	for _, c := range cases {
		if MayChangeSettings(c.emp, c.actor) != c.settings || MayBook(c.emp, c.actor) != c.book || MayErase(c.emp, c.actor) != c.rm {
			t.Errorf("%s: settings %v book %v erase %v", c.name, MayChangeSettings(c.emp, c.actor), MayBook(c.emp, c.actor),
				MayErase(c.emp, c.actor))
		}
	}
}

func TestEmployerHolderValidation(t *testing.T) {
	m := managedEmployer("m1", "acme")
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, e := range []Employer{
		func() Employer { x := m; x.Organization = ""; return x }(),
		func() Employer { x := m; x.Holder = "someone"; return x }(),
		func() Employer { x := employerOf(ModeSchedule, ""); x.Organization = "acme"; return x }(),
	} {
		if err := e.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted", e)
		}
	}
}

func span(emp, date string, startHour, minutes int) Entry {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	day := d(date)
	start := day.At(startHour*60, berlin)
	end := start.Add(time.Duration(minutes) * time.Minute)
	e := Entry{ID: NewID(), Owner: owner, EmployerID: emp, Kind: EntryWork, Start: &start, End: &end, Source: SourceManual}
	_ = e.Normalize()
	return e
}

func TestPersonalOverview(t *testing.T) {
	job := employerOf(ModeExact, "")
	job.ID = "job"
	second := managedEmployer("second", "acme")
	second.Mode = ModeExact
	client := employerOf(ModeExact, "")
	client.ID, client.Kind = "client", KindClient
	weekly := employerOf(ModeWeekSum, "")
	weekly.ID = "weekly"
	cat := Catalog{Employers: []Employer{job, second, client, weekly}}
	entries := []Entry{
		// Monday: 6 h + 5 h for two employers is 11 h: a hint. The client's hours do not count.
		span("job", "2026-03-02", 6, 360), span("second", "2026-03-02", 13, 300), span("client", "2026-03-02", 19, 240),
		// Tuesday: 7 h at the job and 3 h for the client: no hint.
		span("job", "2026-03-03", 8, 420), span("client", "2026-03-03", 16, 180),
		// Wednesday ends at 23:00, Thursday starts at 07:00: 8 h of rest.
		span("second", "2026-03-04", 15, 480), span("job", "2026-03-05", 7, 600),
		// Friday and Saturday push the week past 48 hours.
		span("job", "2026-03-06", 6, 600), span("second", "2026-03-07", 8, 600),
	}
	week := work("2026-03-02", 600)
	week.EmployerID, week.Precision = "weekly", PrecisionWeek
	entries = append(entries, week)
	o, err := PersonalOverview(Data{Catalog: cat, Entries: entries}, d("2026-03-02"), d("2026-03-08"), time.Monday)
	if err != nil {
		t.Fatal(err)
	}
	mon := o.Days[0]
	if mon.Worked != 360+300+240+600 || mon.Employment != 360+300 || mon.PerEmployer["client"] != 240 || mon.PerEmployer["weekly"] != 600 {
		t.Fatalf("Monday: %+v", mon)
	}
	codes := map[HintCode][]Date{}
	for _, h := range o.Hints {
		codes[h.Code] = append(codes[h.Code], h.Date)
	}
	// Monday 11 h; Thursday and Friday 10 h each are not over the limit.
	if got := codes[HintDayOver10h]; len(got) != 1 || got[0] != d("2026-03-02") {
		t.Errorf("day hints: %v", got)
	}
	// 660 + 420 + 480 + 600 + 600 + 600 plus the weekly 600 = 3960 minutes of employment: 66 h.
	if got := codes[HintWeekOver48h]; len(got) != 1 || got[0] != d("2026-03-02") {
		t.Errorf("week hints: %v (%+v)", got, o.Weeks)
	}
	if got := codes[HintRestUnder11h]; len(got) != 1 || got[0] != d("2026-03-05") {
		// Monday's employment ends 18:00 (the client's 19:00 to 23:00 does not count), Tuesday starts 08:00: 14 h.
		// Tuesday ends 15:00, Wednesday starts 15:00. Wednesday ends 23:00, Thursday starts 07:00: 8 h. Thursday ends
		// 17:00, Friday starts 06:00: 13 h. Friday ends 16:00, Saturday starts 08:00: 16 h.
		t.Errorf("rest hints: %v", got)
	}
	if len(o.Weeks) != 1 || o.Weeks[0].Days != 7 {
		t.Errorf("weeks: %+v", o.Weeks)
	}

	// Never for data of several people, such as what an organization gets.
	two := cat
	bob := employerOf(ModeExact, "")
	bob.ID, bob.Owner = "bob", "bob"
	two.Employers = append(two.Employers, bob)
	if _, err := PersonalOverview(Data{Catalog: two}, d("2026-03-02"), d("2026-03-08"), time.Monday); !errors.Is(err, ErrNotPersonal) {
		t.Fatalf("overview over two people: %v", err)
	}
}

// Spans typed for the hours of a day (8-16:30 30m) count for the rest between two days like spans with start and end,
// in the time zone of the entry, across employers, and a span into the next day ends there.
func TestRestHintsFromTypedSpans(t *testing.T) {
	job := employerOf(ModeSchedule, "")
	job.ID, job.TimeZone = "job", "Europe/Berlin"
	second := managedEmployer("second", "acme")
	typed := func(emp, date, text string, minutes int) Entry {
		e := work(date, minutes)
		e.EmployerID, e.Typed, e.TimeZone = emp, text, "Europe/Berlin"
		return e
	}
	cat := Catalog{Employers: []Employer{job, second}}
	entries := []Entry{
		// Monday ends 22:00 at the second employer, Tuesday starts 06:00 at the job: 8 h of rest.
		typed("job", "2026-03-02", "8-16:30 30m", 480), typed("second", "2026-03-02", "18-22", 240),
		typed("job", "2026-03-03", "6-12 13-15", 480),
		// Wednesday's night shift ends Thursday 06:00, Thursday's next work starts 14:00 the same day: Thursday's work
		// begins 8 h after the end; Friday starts 07:00 after Thursday ends 18:00: 13 h.
		typed("job", "2026-03-04", "22-6", 480), typed("job", "2026-03-05", "14-18", 240),
		typed("job", "2026-03-06", "7-15", 480),
		// Hours typed as hours say nothing about the clock.
		work("2026-03-07", 300),
	}
	entries[len(entries)-1].EmployerID = "job"
	o, err := PersonalOverview(Data{Catalog: cat, Entries: entries}, d("2026-03-02"), d("2026-03-08"), time.Monday)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range o.Hints {
		if h.Code == HintRestUnder11h {
			got = append(got, h.Date.String()+"/"+strconv.Itoa(h.Minutes))
		}
	}
	if strings.Join(got, " ") != "2026-03-03/480 2026-03-05/480" {
		t.Errorf("rest hints: %v", got)
	}
	spans := TypedSpans("8-16:30 30m, 22-6; Pause 7.15-7.45")
	if len(spans) != 3 || spans[0] != (ClockSpan{480, 990}) || spans[1] != (ClockSpan{1320, 1800}) || spans[2] != (ClockSpan{435, 465}) {
		t.Errorf("spans: %+v", spans)
	}
}

type miniStore struct{ Store }

func TestPrivatePartAndImport(t *testing.T) {
	m := managedEmployer("m1", "acme")
	p := employerOf(ModeSchedule, "")
	p.ID = "p1"
	x := Export{Format: ExportFormat, Owner: owner, Catalog: Catalog{Employers: []Employer{m, p},
		Schedules: []Schedule{func() Schedule { s := week("sm", "2026-01-01", 480); s.EmployerID = "m1"; return s }(),
			week("sp", "2026-01-01", 480)},
		PeriodCloses: []PeriodClose{{ID: "pc", Owner: owner, EmployerID: "p1", From: d("2026-01-01"), To: d("2026-01-31")}},
		Tags:         []Tag{{ID: "t", Owner: owner, Path: "x"}}},
		Entries: []Entry{func() Entry { e := work("2026-01-05", 60); e.EmployerID = "m1"; return e }(),
			func() Entry { e := work("2026-01-05", 90); e.EmployerID = "p1"; return e }()},
		Absences: []Absence{func() Absence { a := absence(AbsenceSick, "2026-02-02", "2026-02-02"); a.EmployerID = ""; return a }(),
			func() Absence {
				a := absence(AbsenceVacation, "2026-02-03", "2026-02-03")
				a.EmployerID = "m1"
				return a
			}()},
	}
	x.Revisions = []Revision{{ID: "r1", Owner: owner, RecordID: x.Entries[0].ID}, {ID: "r2", Owner: owner, RecordID: x.Entries[1].ID}}
	priv := x.PrivatePart()
	if len(priv.Catalog.Employers) != 1 || priv.Catalog.Employers[0].ID != "p1" || len(priv.Catalog.Schedules) != 1 ||
		len(priv.Entries) != 1 || priv.Entries[0].Minutes != 90 || len(priv.Absences) != 1 || len(priv.Revisions) != 1 ||
		len(priv.Catalog.Tags) != 1 || len(priv.Catalog.PeriodCloses) != 1 {
		t.Fatalf("private part: %+v", priv)
	}

	// The import goes through the store interface; memstore lives in another package, so a tiny fake records calls.
	rec := &recorder{}
	if err := ImportPrivate(context.Background(), rec, x, "alice2"); err != nil {
		t.Fatal(err)
	}
	if rec.entries != 1 || rec.absences != 1 || rec.last != RecPeriodClose || rec.owners["alice2"] == 0 || len(rec.owners) != 1 {
		t.Fatalf("import: %+v", rec)
	}
}

// recorder is a Store that only counts what an import writes, and refuses entries after a period close, like a store.
type recorder struct {
	miniStore
	entries, absences int
	closed            bool
	last              Kind
	owners            map[string]int
}

func (r *recorder) Put(_ context.Context, rec Record) error {
	if r.owners == nil {
		r.owners = map[string]int{}
	}
	r.owners[rec.Meta().Owner]++
	r.last = rec.Meta().Kind
	if rec.Meta().Kind == RecPeriodClose {
		r.closed = true
	}
	return rec.Validate()
}

func (r *recorder) CreateEntry(_ context.Context, e Entry, c Change) (Entry, error) {
	if r.closed {
		return Entry{}, ErrLocked
	}
	r.entries++
	r.owners[e.Owner]++
	e, _, err := PrepareNewEntry(e, c)
	return e, err
}

func (r *recorder) CreateAbsence(_ context.Context, a Absence, c Change) (Absence, error) {
	if r.closed {
		return Absence{}, ErrLocked
	}
	r.absences++
	r.owners[a.Owner]++
	a, _, err := PrepareNewAbsence(a, DefaultAbsenceTypes(), c)
	return a, err
}
