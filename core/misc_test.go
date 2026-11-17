// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDate(t *testing.T) {
	if NewDate(2026, 2, 30) != d("2026-03-02") || d("2026-01-31").AddMonths(1) != d("2026-03-03") {
		t.Fatal("normalisation")
	}
	if d("2026-03-29").AddDays(1) != d("2026-03-30") || d("2026-03-01").DaysUntil(d("2026-04-01")) != 31 ||
		d("2026-10-25").DaysUntil(d("2026-10-26")) != 1 {
		t.Fatal("arithmetic across daylight saving")
	}
	if d("2026-11-19").StartOfWeek(time.Monday) != d("2026-11-16") || d("2026-11-19").StartOfWeek(time.Sunday) != d("2026-11-15") {
		t.Fatal("start of week")
	}
	if !d("2026-01-05").Within(d("2026-01-01"), Date{}) || d("2025-12-31").Within(d("2026-01-01"), Date{}) {
		t.Fatal("open range")
	}
	var x struct{ D Date }
	if err := json.Unmarshal([]byte(`{"D":"2026-11-17"}`), &x); err != nil || x.D != d("2026-11-17") {
		t.Fatalf("json: %v %v", x, err)
	}
	if err := json.Unmarshal([]byte(`{"D":"17.11.2026"}`), &x); err == nil {
		t.Fatal("bad date accepted")
	}
	b, _ := json.Marshal(struct{ D Date }{})
	if string(b) != `{"D":""}` {
		t.Fatalf("zero date: %s", b)
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	if got := d("2026-03-29").At(150, berlin); got.Hour() != 3 || got.Minute() != 30 {
		t.Fatalf("02:30 does not exist on 2026-03-29 in Berlin, Go moves it: %v", got)
	}
}

func TestRetention(t *testing.T) {
	r := Retention{Class: ClassTimeRecords, YearsAfterYearEnd: 3}
	if r.Cutoff(d("2026-11-17")) != d("2023-01-01") || r.Cutoff(d("2026-01-01")) != d("2023-01-01") {
		t.Fatal("cutoff")
	}
	for _, bad := range []Retention{{ClassTimeRecords, 1}, {ClassTimeRecords, 101}, {"x", 3}} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted", bad)
		}
	}
	for _, r := range DefaultRetention() {
		if err := r.Validate(); err != nil || r.YearsAfterYearEnd != 3 {
			t.Errorf("default %+v: %v", r, err)
		}
	}
	org := Organization{ID: "o", Name: "O", Retention: []Retention{{ClassTimeRecords, 5}}}
	if org.RetentionFor(ClassTimeRecords).YearsAfterYearEnd != 5 || org.RetentionFor(ClassLeaveNotices).YearsAfterYearEnd != 3 {
		t.Fatal("organization retention")
	}
	org.Retention[0].YearsAfterYearEnd = 1
	if err := org.Validate(); err == nil {
		t.Fatal("an organization kept time records for one year")
	}
}

func TestOrganization(t *testing.T) {
	now := time.Now()
	org := Organization{ID: "acme", Name: "ACME", Templates: []ScheduleTemplate{{Name: "Part", Mode: ModeSchedule}}}
	if err := org.Validate(); err != nil {
		t.Fatal(err)
	}
	org.Features = CompanyFeatures{Approval: true, Agreement: "Works agreement 2026-10, all staff"}
	if err := org.Validate(); err == nil {
		t.Fatal("features on without confirmation")
	}
	org.Features.ConfirmedBy, org.Features.ConfirmedAt = "boss", &now
	if err := org.Validate(); err != nil {
		t.Fatal(err)
	}
	if (CompanyFeatures{}).Any() {
		t.Fatal("features are on by default")
	}
	if _, _, _, err := Join(org, "alice", RoleMember, "Full", d("2026-01-01")); err == nil {
		t.Fatal("unknown template")
	}
	m, emp, sched, err := Join(org, "alice", RoleMember, "", d("2026-01-01"))
	if err != nil || sched != nil || !emp.Managed() || emp.Organization != "acme" || m.EmployerID != emp.ID {
		t.Fatalf("join: %+v %+v %v", m, emp, err)
	}
	if _, _, err := EndMembership(m, emp, d("2025-12-31")); err == nil {
		t.Fatal("ended before it started")
	}
	dup := org
	dup.Templates = append(dup.Templates, ScheduleTemplate{Name: "Part", Mode: ModeSchedule})
	if err := dup.Validate(); err == nil {
		t.Fatal("two templates with one name")
	}
	ms := []Membership{{ID: "1", Organization: "acme", Person: "bob", Role: RoleAdmin, From: d("2026-01-01"), Until: d("2026-03-31")}}
	if a := ActorFor("bob", "acme", ms, d("2026-04-01")); a.Admin || a.Organization != "" {
		t.Fatalf("ended admin still admin: %+v", a)
	}
	if a := ActorFor("bob", "acme", ms, d("2026-03-31")); !a.Admin {
		t.Fatalf("admin on the last day: %+v", a)
	}
}

func TestExport(t *testing.T) {
	start := time.Date(2026, 3, 2, 7, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	e := work("2026-03-02", 60)
	e.Start, e.End, e.Note, e.Tags = &start, &end, "=HYPERLINK(\"http://example.com\")", []string{"a", "b/c"}
	e.Rate = &RateSnapshot{RuleID: "r", Amount: 2500, Currency: "EUR"}
	var buf bytes.Buffer
	if err := WriteEntriesCSV(&buf, []Entry{e}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(rows) != 2 || strings.Join(rows[0], ",") != strings.Join(EntryColumns, ",") {
		t.Fatalf("csv: %v %v", rows, err)
	}
	row := map[string]string{}
	for i, c := range EntryColumns {
		row[c] = rows[1][i]
	}
	if row["note"] != `'=HYPERLINK("http://example.com")` || row["tags"] != "a b/c" || row["start"] != "2026-03-02T07:00:00Z" ||
		row["rate"] != "2500" || row["minutes"] != "60" {
		t.Fatalf("row: %v", row)
	}
	x := Export{Format: ExportFormat, Owner: owner, Entries: []Entry{e}}
	buf.Reset()
	if err := x.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var back Export
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil || len(back.Entries) != 1 || back.Entries[0].Note != e.Note ||
		!back.Entries[0].Start.Equal(start) {
		t.Fatalf("json round trip: %+v %v", back, err)
	}
}

func TestValidation(t *testing.T) {
	good := work("2026-03-02", 60)
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 3, 2, 7, 0, 0, 0, time.UTC)
	for name, mut := range map[string]func(*Entry){
		"bad id":             func(e *Entry) { e.ID = "a b" },
		"no employer":        func(e *Entry) { e.EmployerID = "" },
		"no date":            func(e *Entry) { e.Date = Date{} },
		"unknown kind":       func(e *Entry) { e.Kind = "sleep" },
		"negative work":      func(e *Entry) { e.Minutes = -1 },
		"span without times": func(e *Entry) { e.Precision = PrecisionSpan },
		"end before start": func(e *Entry) {
			end := start.Add(-time.Minute)
			e.Start, e.End, e.Precision = &start, &end, PrecisionSpan
		},
		"span over a day": func(e *Entry) {
			end := start.Add(25 * time.Hour)
			e.Start, e.End, e.Precision = &start, &end, PrecisionSpan
		},
		"adjust with times": func(e *Entry) {
			end := start.Add(time.Hour)
			e.Kind, e.Start, e.End = EntryAdjust, &start, &end
		},
		"bad tag":       func(e *Entry) { e.Tags = []string{"has space"} },
		"bad currency":  func(e *Entry) { e.Rate = &RateSnapshot{Currency: "euro"} },
		"long note":     func(e *Entry) { e.Note = strings.Repeat("x", 2001) },
		"bad source":    func(e *Entry) { e.Source = "magic" },
		"bad precision": func(e *Entry) { e.Precision = "minute" },
	} {
		e := good
		mut(&e)
		if err := e.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted", name)
		}
	}
	for name, r := range map[string]Record{
		"pay rule without currency":    PayRule{ID: "r", Owner: owner, EmployerID: "e1", ValidFrom: d("2026-01-01"), Basis: PayHourly},
		"pay rule ending before start": PayRule{ID: "r", Owner: owner, EmployerID: "e1", ValidFrom: d("2026-02-01"), ValidTo: d("2026-01-01"), Basis: PayHourly, Currency: "EUR"},
		"per project without project":  PayRule{ID: "r", Owner: owner, EmployerID: "e1", ValidFrom: d("2026-01-01"), Basis: PayPerProject, Currency: "EUR"},
		"schedule over 24 h":           func() Schedule { s := week("s", "2026-01-01", 1500); return s }(),
		"schedule start after end": func() Schedule {
			s := week("s", "2026-01-01", 60)
			s.Days[1].Start, s.Days[1].End = ip(600), ip(500)
			return s
		}(),
		"unknown region":        Employer{ID: "e", Owner: owner, Name: "X", Kind: KindEmployer, Mode: ModeExact, Region: "DE-XX"},
		"unknown time zone":     Employer{ID: "e", Owner: owner, Name: "X", Kind: KindEmployer, Mode: ModeExact, TimeZone: "Mars/Olympus"},
		"adjustment w/o reason": Adjustment{ID: "a", Owner: owner, EmployerID: "e1", Date: d("2026-01-01"), Kind: AdjustOvertime},
		"holiday fraction 0":    Holiday{ID: "h", Owner: owner, Date: d("2026-01-01"), Name: "X"},
		"mode change w/o date":  ModeChange{ID: "m", Owner: owner, EmployerID: "e1", Mode: ModePlan},
		"confirmation reversed": Confirmation{ID: "c", Owner: owner, EmployerID: "e1", From: d("2026-01-02"), To: d("2026-01-01")},
		"leave notice w/o time": LeaveNotice{ID: "n", Owner: owner, EmployerID: "e1", Year: 2026},
		"leave notice too long": LeaveNotice{ID: "n", Owner: owner, EmployerID: "e1", Year: 2026, NotifiedAt: time.Now(),
			Text: strings.Repeat("x", 2001)},
		"absence type bad effect": AbsenceType{ID: "t", Owner: owner, Key: "x", Name: "X", Effect: "maybe"},
	} {
		if err := r.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted", name)
		}
	}
}
