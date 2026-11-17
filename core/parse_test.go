// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func ip(v int) *int { return &v }

// The reference date is Tuesday, 2026-11-17; its week starts on Monday, 2026-11-16.
var today = d("2026-11-17")

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Parsed
	}{
		{"2h #meeting @ACME/Website write the offer", Parsed{Minutes: 120, Tags: []string{"meeting"}, Project: "ACME/Website",
			Note: "write the offer"}},
		{"8-16:30 30m Pause", Parsed{Start: ip(480), End: ip(990), Minutes: 510, Break: 30}},
		{"8-16:30 Pause 30m", Parsed{Start: ip(480), End: ip(990), Minutes: 510, Break: 30}},
		{"08:00-12:15", Parsed{Start: ip(480), End: ip(735), Minutes: 255}},
		{"13.10-13.45 call", Parsed{Start: ip(790), End: ip(825), Minutes: 35, Note: "call"}},
		{"22-6 night shift", Parsed{Start: ip(1320), End: ip(1800), Minutes: 480, Note: "night shift"}},
		{"0-24", Parsed{Start: ip(0), End: ip(1440), Minutes: 1440}},
		{"2.5h", Parsed{Minutes: 150}},
		{"2,5h", Parsed{Minutes: 150}},
		{"2h30m", Parsed{Minutes: 150}},
		{"2h30", Parsed{Minutes: 150}},
		{"2:30", Parsed{Minutes: 150}},
		{"90min", Parsed{Minutes: 90}},
		{"45m", Parsed{Minutes: 45}},
		{"38:30", Parsed{Minutes: 2310}},
		{"Fr -2h früher weg", Parsed{Date: d("2026-11-20"), Minutes: -120, Adjust: true, Note: "früher weg"}},
		{"Fr 2h früher weg", Parsed{Date: d("2026-11-20"), Minutes: -120, Adjust: true, Note: "früher weg"}},
		{"Do +1:30 Baustelle Meier", Parsed{Date: d("2026-11-19"), Minutes: 90, Adjust: true, Note: "Baustelle Meier"}},
		{"1h länger", Parsed{Minutes: 60, Adjust: true, Note: "länger"}},
		{"left early 30m", Parsed{Minutes: -30, Adjust: true, Note: "left early"}},
		{"Mi krank", Parsed{Date: d("2026-11-18"), To: d("2026-11-18"), Absence: AbsenceSick}},
		{"Mo-Mi Urlaub", Parsed{Date: d("2026-11-16"), To: d("2026-11-18"), Absence: AbsenceVacation}},
		{"24.12. halb Urlaub", Parsed{Date: d("2026-12-24"), To: d("2026-12-24"), Absence: AbsenceVacation, HalfDay: true}},
		{"17.11.2026-20.11.2026 vacation Lisbon", Parsed{Date: d("2026-11-17"), To: d("2026-11-20"), Absence: AbsenceVacation,
			Note: "Lisbon"}},
		{"Zeitausgleich", Parsed{Absence: AbsenceComp}},
		{"gestern 13:10-13:45 Telefonat #kunde", Parsed{Date: d("2026-11-16"), Start: ip(790), End: ip(825), Minutes: 35,
			Tags: []string{"kunde"}, Note: "Telefonat"}},
		{"yesterday 1h", Parsed{Date: d("2026-11-16"), Minutes: 60}},
		{"tomorrow 1h", Parsed{Date: d("2026-11-18"), Minutes: 60}},
		{"So 1h", Parsed{Date: d("2026-11-22"), Minutes: 60}},
		{"2026-03-29 1h", Parsed{Date: d("2026-03-29"), Minutes: 60}},
		{"1h so do it", Parsed{Minutes: 60, Note: "so do it"}},
		{"30m break", Parsed{Break: 30}},
		{"8h 30m Pause", Parsed{Minutes: 480, Break: 30}},
		{"1h #a #a #b/c", Parsed{Minutes: 60, Tags: []string{"a", "b/c"}}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := Parse(c.in, today)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("\n got  %s\n want %s", show(got), show(c.want))
			}
		})
	}
}

func show(p Parsed) string {
	s := strings.Builder{}
	s.WriteString(p.Date.String() + ".." + p.To.String())
	if p.Start != nil {
		s.WriteString(" start=" + time.Duration(*p.Start*int(time.Minute)).String())
	}
	if p.End != nil {
		s.WriteString(" end=" + time.Duration(*p.End*int(time.Minute)).String())
	}
	return s.String() + " " + strings.TrimSpace(strings.Join([]string{
		"min=" + time.Duration(p.Minutes*int(time.Minute)).String(), "adjust=" + boolStr(p.Adjust),
		"break=" + time.Duration(p.Break*int(time.Minute)).String(), "absence=" + p.Absence, "half=" + boolStr(p.HalfDay),
		"tags=" + strings.Join(p.Tags, ","), "project=" + p.Project, "note=" + p.Note}, " "))
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"", "   ", "hello", "Mo",
		"25-26", "8:60-9", "8-8", "24:30-1",
		"2h 3h", "8-16 2h", "8-12 13-17", "1h -30m",
		"krank 2h", "krank Grippe", "Mo-Mi 2h", "halb 2h", "-30m Pause",
		"früher länger 1h", "1h @a @b", "1h #bad#tag", "8-9 1h Pause",
		"Mi-Mo Urlaub", "31.02. Urlaub", "2026-13-01 1h", "0h",
		"170:00", "-25h", "1500m Pause",
		strings.Repeat("x", MaxInput+1), "\xff\xfe",
	} {
		if p, err := Parse(in, today); err == nil {
			t.Errorf("%q parsed: %s", in, show(p))
		} else {
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Errorf("%q: error %T, want *ParseError", in, err)
			}
		}
	}
}

func TestParsedEntries(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	opts := func(date string) EntryOptions {
		return EntryOptions{Owner: owner, EmployerID: "e1", ProjectID: "p1", Location: berlin, Today: d(date)}
	}
	cases := []struct {
		in, date string
		minutes  []int // per entry
		kinds    []EntryKind
	}{
		{"8-16:30 30m Pause", "2026-11-17", []int{510, 30}, []EntryKind{EntryWork, EntryBreak}},
		// The night the clocks go back: 22:00 to 06:00 is nine hours of real time.
		{"22-6", "2026-10-24", []int{540}, []EntryKind{EntryWork}},
		// The night the clocks go forward: 00:00 to 06:00 is five hours.
		{"0-6", "2026-03-29", []int{300}, []EntryKind{EntryWork}},
		{"2h #x", "2026-03-29", []int{120}, []EntryKind{EntryWork}},
		{"-2h", "2026-03-29", []int{-120}, []EntryKind{EntryAdjust}},
		{"30m Pause", "2026-03-29", []int{30}, []EntryKind{EntryBreak}},
	}
	for _, c := range cases {
		p, err := Parse(c.in, d(c.date))
		if err != nil {
			t.Fatal(err)
		}
		es, err := p.Entries(opts(c.date))
		if err != nil {
			t.Fatal(err)
		}
		if len(es) != len(c.minutes) {
			t.Fatalf("%s: %d entries", c.in, len(es))
		}
		for i, e := range es {
			e.ID = NewID()
			if err := e.Validate(); err != nil {
				t.Fatalf("%s: %v", c.in, err)
			}
			if e.Minutes != c.minutes[i] || e.Kind != c.kinds[i] || e.Date != d(c.date) || e.Source != SourceParse {
				t.Errorf("%s entry %d: %+v", c.in, i, e)
			}
		}
	}
	p, _ := Parse("Mo-Mi Urlaub", today)
	if _, err := p.Entries(opts("2026-11-17")); err == nil {
		t.Fatal("an absence became entries")
	}
	a, err := p.AbsenceRecord(owner, "", today)
	if err != nil || a.From != d("2026-11-16") || a.To != d("2026-11-18") || a.Type != AbsenceVacation {
		t.Fatalf("absence: %+v %v", a, err)
	}
	if _, err := (Parsed{Minutes: 60}).AbsenceRecord(owner, "", today); err == nil {
		t.Fatal("work became an absence")
	}
}

// FuzzParse: Parse never panics, and whatever it accepts is consistent and becomes valid entries or a valid absence.
func FuzzParse(f *testing.F) {
	for _, s := range []string{"2h #tag @Project note", "8-16:30 30m Pause", "Fr -2h früher weg", "Mo-Mi Urlaub",
		"24.12. halb Urlaub", "22-6", "2:30", "2,5h", "gestern 13:10-13:45 x", "17.11.-20.11. vacation", "30m break",
		"0-24", "+1h30m", "1h #a/b/c @x/y z"} {
		f.Add(s)
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, in string) {
		p, err := Parse(in, today)
		if err != nil {
			return
		}
		if (p.Start == nil) != (p.End == nil) {
			t.Fatalf("%q: start without end", in)
		}
		if p.Start != nil && (*p.Start < 0 || *p.Start >= 24*60+1 || *p.End <= *p.Start || *p.End > *p.Start+24*60) {
			t.Fatalf("%q: span %d-%d", in, *p.Start, *p.End)
		}
		if p.Break < 0 || (!p.Adjust && p.Minutes < 0) {
			t.Fatalf("%q: negative time", in)
		}
		if p.Absence != "" {
			a, err := p.AbsenceRecord(owner, "e1", today)
			if err != nil {
				t.Fatal(err)
			}
			a.ID, a.Status = NewID(), StatusApproved
			if err := a.Validate(DefaultAbsenceTypes()); err != nil {
				t.Fatalf("%q: absence invalid: %v", in, err)
			}
			return
		}
		es, err := p.Entries(EntryOptions{Owner: owner, EmployerID: "e1", Location: berlin, Today: today})
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if len(es) == 0 {
			t.Fatalf("%q: accepted, but no entry", in)
		}
		for _, e := range es {
			e.ID = NewID()
			if err := e.Validate(); err != nil {
				t.Fatalf("%q: entry invalid: %v (%+v)", in, err, e)
			}
		}
	})
}
