// SPDX-License-Identifier: MIT

// Package storetest is the conformance test of core.Store: every implementation runs it, so the in-memory store and
// the PostgreSQL store follow the same rules. An embedding application with a store of its own can run it, too.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

// Run runs every case against fresh stores from newStore.
func Run(t *testing.T, newStore func(t *testing.T) core.Store) {
	cases := []struct {
		name string
		fn   func(*testing.T, core.Store)
	}{
		{"catalog", catalog},
		{"entry history", entryHistory},
		{"entry conflict", entryConflict},
		{"entry filter", entryFilter},
		{"owners are separate", ownersSeparate},
		{"closed period", closedPeriod},
		{"absences", absences},
		{"purge", purge},
		{"erase", erase},
		{"managed and private are separate", managedSeparate},
		{"organizations", organizations},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.fn(t, newStore(t)) })
	}
}

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got error %v, want %v", err, want)
	}
}

func employer(owner, id string) core.Employer {
	return core.Employer{ID: id, Owner: owner, Name: "Example GmbH", Kind: core.KindEmployer, Mode: core.ModeSchedule,
		Region: "DE-BY", WeekStart: time.Monday}
}

func entry(owner, employerID, date string, minutes int) core.Entry {
	return core.Entry{Owner: owner, EmployerID: employerID, Kind: core.EntryWork, Date: core.MustDate(date),
		Minutes: minutes, Tags: []string{"site/a"}, Note: "note"}
}

func catalog(t *testing.T, s core.Store) {
	emp := employer("alice", "e1")
	must(t, s.Put(ctx, emp))
	start := 8 * 60
	end := 16*60 + 30
	sched := core.Schedule{ID: "s1", Owner: "alice", EmployerID: "e1", ValidFrom: core.MustDate("2026-01-01"),
		LeaveDays: 30, OvertimeBasis: core.BasisMonth}
	sched.Days[time.Monday] = core.DayPlan{Minutes: 480, Start: &start, End: &end, Break: 30}
	must(t, s.Put(ctx, sched))
	must(t, s.Put(ctx, core.PayRule{ID: "r1", Owner: "alice", EmployerID: "e1", ValidFrom: core.MustDate("2026-01-01"),
		Basis: core.PayHourly, Amount: 2500, Currency: "EUR"}))
	must(t, s.Put(ctx, core.Tag{ID: "t1", Owner: "alice", Path: "site/a"}))
	wantErr(t, s.Put(ctx, core.Tag{ID: "t2", Owner: "alice", Path: "bad tag"}), core.ErrInvalid)

	c, err := s.Catalog(ctx, "alice")
	must(t, err)
	if len(c.Employers) != 1 || len(c.Schedules) != 1 || len(c.PayRules) != 1 || len(c.Tags) != 1 {
		t.Fatalf("catalog: %+v", c)
	}
	if got := c.Schedules[0].Days[time.Monday]; got.Minutes != 480 || *got.Start != start || *got.End != end {
		t.Fatalf("schedule came back as %+v", got)
	}

	// Replace by ID.
	emp.Name = "Renamed"
	must(t, s.Put(ctx, emp))
	c, err = s.Catalog(ctx, "alice")
	must(t, err)
	if len(c.Employers) != 1 || c.Employers[0].Name != "Renamed" {
		t.Fatalf("replace: %+v", c.Employers)
	}

	// Another owner sees nothing and cannot take the ID over.
	other, err := s.Catalog(ctx, "bob")
	must(t, err)
	if len(other.Employers) != 0 {
		t.Fatal("bob sees alice's employer")
	}
	wantErr(t, s.Put(ctx, employer("bob", "e1")), core.ErrNotFound)
	wantErr(t, s.Delete(ctx, core.RecTag, "bob", "t1"), core.ErrNotFound)

	must(t, s.Delete(ctx, core.RecTag, "alice", "t1"))
	wantErr(t, s.Delete(ctx, core.RecTag, "alice", "t1"), core.ErrNotFound)

	// Closed periods stay.
	pc := core.PeriodClose{ID: "p1", Owner: "alice", EmployerID: "e1", From: core.MustDate("2026-01-01"),
		To: core.MustDate("2026-01-31")}
	must(t, s.Put(ctx, pc))
	wantErr(t, s.Delete(ctx, core.RecPeriodClose, "alice", "p1"), core.ErrLocked)
	wantErr(t, s.Put(ctx, pc), core.ErrLocked)
}

func entryHistory(t *testing.T, s core.Store) {
	must(t, s.Put(ctx, employer("alice", "e1")))
	e, err := s.CreateEntry(ctx, entry("alice", "e1", "2026-03-02", 480), core.Change{By: "alice"})
	must(t, err)
	if e.ID == "" || e.Version != 1 || e.Precision != core.PrecisionDay || e.Source != core.SourceManual {
		t.Fatalf("created: %+v", e)
	}

	e.Minutes = 450
	e.Note = "corrected"
	e2, err := s.UpdateEntry(ctx, e, core.Change{By: "alice", Reason: "forgot the doctor"})
	must(t, err)
	if e2.Version != 2 || e2.Minutes != 450 || !e2.Created.Equal(e.Created) {
		t.Fatalf("updated: %+v", e2)
	}

	must(t, s.DeleteEntry(ctx, "alice", e.ID, 2, core.Change{By: "alice", Reason: "double"}))
	got, err := s.Entry(ctx, "alice", e.ID)
	must(t, err)
	if got.DeletedAt == nil || got.Version != 3 {
		t.Fatalf("deleted entry: %+v", got)
	}
	live, err := s.Entries(ctx, core.EntryFilter{Owner: "alice"})
	must(t, err)
	if len(live) != 0 {
		t.Fatalf("deleted entry still listed: %+v", live)
	}
	all, err := s.Entries(ctx, core.EntryFilter{Owner: "alice", IncludeDeleted: true})
	must(t, err)
	if len(all) != 1 {
		t.Fatalf("with deleted: %d", len(all))
	}

	revs, err := s.Revisions(ctx, "alice", e.ID)
	must(t, err)
	if len(revs) != 3 {
		t.Fatalf("want 3 revisions, got %d", len(revs))
	}
	ops := []core.Operation{core.OpCreate, core.OpUpdate, core.OpDelete}
	for i, r := range revs {
		if r.Op != ops[i] || r.Version != i+1 || r.Type != core.RecordEntry {
			t.Fatalf("revision %d: %+v", i, r)
		}
	}
	// The original value stays readable.
	var before core.Entry
	must(t, json.Unmarshal(revs[1].Before, &before))
	if before.Minutes != 480 || before.Note != "note" || revs[1].Reason != "forgot the doctor" || revs[1].By != "alice" {
		t.Fatalf("history lost the original: %+v / %+v", before, revs[1])
	}
	if revs[0].Before != nil {
		t.Fatal("a creation has no previous state")
	}
}

func entryConflict(t *testing.T, s core.Store) {
	must(t, s.Put(ctx, employer("alice", "e1")))
	e, err := s.CreateEntry(ctx, entry("alice", "e1", "2026-03-02", 480), core.Change{})
	must(t, err)
	_, err = s.CreateEntry(ctx, e, core.Change{})
	wantErr(t, err, core.ErrConflict)

	stale := e
	e.Minutes = 400
	_, err = s.UpdateEntry(ctx, e, core.Change{})
	must(t, err)
	stale.Minutes = 300
	_, err = s.UpdateEntry(ctx, stale, core.Change{})
	wantErr(t, err, core.ErrConflict)
	wantErr(t, s.DeleteEntry(ctx, "alice", e.ID, 1, core.Change{}), core.ErrConflict)

	bad := entry("alice", "e1", "2026-03-02", -5)
	_, err = s.CreateEntry(ctx, bad, core.Change{})
	wantErr(t, err, core.ErrInvalid)
	_, err = s.UpdateEntry(ctx, entry("alice", "e1", "2026-03-02", 5), core.Change{})
	wantErr(t, err, core.ErrNotFound)
}

func entryFilter(t *testing.T, s core.Store) {
	must(t, s.Put(ctx, employer("alice", "e1")))
	must(t, s.Put(ctx, employer("alice", "e2")))
	loc, _ := time.LoadLocation("Europe/Berlin")
	start := time.Date(2026, 3, 3, 23, 30, 0, 0, loc)
	end := start.Add(90 * time.Minute)
	span := core.Entry{Owner: "alice", EmployerID: "e1", Kind: core.EntryWork, Start: &start, End: &end,
		TimeZone: "Europe/Berlin"}
	for _, e := range []core.Entry{entry("alice", "e1", "2026-03-01", 60), entry("alice", "e1", "2026-03-04", 60),
		entry("alice", "e2", "2026-03-02", 60), span} {
		_, err := s.CreateEntry(ctx, e, core.Change{})
		must(t, err)
	}
	got, err := s.Entries(ctx, core.EntryFilter{Owner: "alice", EmployerID: "e1", From: core.MustDate("2026-03-02"),
		To: core.MustDate("2026-03-04")})
	must(t, err)
	if len(got) != 2 || got[0].Date != core.MustDate("2026-03-03") || got[1].Date != core.MustDate("2026-03-04") {
		t.Fatalf("filter: %+v", got)
	}
	// A span past midnight belongs to the local date of its start and keeps its minutes and times.
	if got[0].Minutes != 90 || got[0].Precision != core.PrecisionSpan || !got[0].Start.Equal(start) || !got[0].End.Equal(end) {
		t.Fatalf("span: %+v", got[0])
	}
	if len(got[0].Tags) != 0 {
		t.Fatalf("tags appeared: %v", got[0].Tags)
	}
}

func ownersSeparate(t *testing.T, s core.Store) {
	must(t, s.Put(ctx, employer("alice", "e1")))
	e, err := s.CreateEntry(ctx, entry("alice", "e1", "2026-03-02", 480), core.Change{})
	must(t, err)
	_, err = s.Entry(ctx, "bob", e.ID)
	wantErr(t, err, core.ErrNotFound)
	stolen := e
	stolen.Owner = "bob"
	_, err = s.UpdateEntry(ctx, stolen, core.Change{})
	wantErr(t, err, core.ErrNotFound)
	wantErr(t, s.DeleteEntry(ctx, "bob", e.ID, 1, core.Change{}), core.ErrNotFound)
	list, err := s.Entries(ctx, core.EntryFilter{Owner: "bob"})
	must(t, err)
	revs, err := s.Revisions(ctx, "bob", "")
	must(t, err)
	if len(list) != 0 || len(revs) != 0 {
		t.Fatal("bob sees alice's entries or history")
	}
}

func closedPeriod(t *testing.T, s core.Store) {
	must(t, s.Put(ctx, employer("alice", "e1")))
	open, err := s.CreateEntry(ctx, entry("alice", "e1", "2026-02-02", 480), core.Change{})
	must(t, err)
	inside, err := s.CreateEntry(ctx, entry("alice", "e1", "2026-01-15", 480), core.Change{})
	must(t, err)
	must(t, s.Put(ctx, core.PeriodClose{ID: "p1", Owner: "alice", EmployerID: "e1", From: core.MustDate("2026-01-01"),
		To: core.MustDate("2026-01-31"), ClosedAt: time.Now()}))

	_, err = s.CreateEntry(ctx, entry("alice", "e1", "2026-01-20", 60), core.Change{})
	wantErr(t, err, core.ErrLocked)
	inside.Minutes = 1
	_, err = s.UpdateEntry(ctx, inside, core.Change{})
	wantErr(t, err, core.ErrLocked)
	wantErr(t, s.DeleteEntry(ctx, "alice", inside.ID, 1, core.Change{}), core.ErrLocked)
	// Moving an open entry into the closed period is refused as well.
	open.Date = core.MustDate("2026-01-30")
	_, err = s.UpdateEntry(ctx, open, core.Change{})
	wantErr(t, err, core.ErrLocked)

	_, err = s.CreateAbsence(ctx, core.Absence{Owner: "alice", EmployerID: "e1", Type: core.AbsenceVacation,
		From: core.MustDate("2026-01-30"), To: core.MustDate("2026-02-03")}, core.Change{})
	wantErr(t, err, core.ErrLocked)
}

func absences(t *testing.T, s core.Store) {
	must(t, s.Put(ctx, employer("alice", "e1")))
	must(t, s.Put(ctx, employer("alice", "e2")))
	a, err := s.CreateAbsence(ctx, core.Absence{Owner: "alice", EmployerID: "e1", Type: core.AbsenceVacation,
		From: core.MustDate("2026-08-03"), To: core.MustDate("2026-08-14"), Note: "Baltic Sea"}, core.Change{By: "alice"})
	must(t, err)
	if a.Status != core.StatusApproved || a.Version != 1 {
		t.Fatalf("absence: %+v", a)
	}
	_, err = s.CreateAbsence(ctx, core.Absence{Owner: "alice", Type: core.AbsenceSick, From: core.MustDate("2026-09-01"),
		To: core.MustDate("2026-09-02"), Note: "flu"}, core.Change{})
	wantErr(t, err, core.ErrInvalid)
	sick, err := s.CreateAbsence(ctx, core.Absence{Owner: "alice", Type: core.AbsenceSick,
		From: core.MustDate("2026-09-01"), To: core.MustDate("2026-09-02")}, core.Change{})
	must(t, err)
	_, err = s.CreateAbsence(ctx, core.Absence{Owner: "alice", Type: "unknown", From: core.MustDate("2026-09-01"),
		To: core.MustDate("2026-09-01")}, core.Change{})
	wantErr(t, err, core.ErrInvalid)

	// Overlap filter: e2 sees the absence for every employer, not the one of e1.
	got, err := s.Absences(ctx, core.AbsenceFilter{Owner: "alice", EmployerID: "e2", From: core.MustDate("2026-08-10"),
		To: core.MustDate("2026-09-30")})
	must(t, err)
	if len(got) != 1 || got[0].ID != sick.ID {
		t.Fatalf("filter e2: %+v", got)
	}
	got, err = s.Absences(ctx, core.AbsenceFilter{Owner: "alice", From: core.MustDate("2026-08-14"),
		To: core.MustDate("2026-08-14")})
	must(t, err)
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("filter on the last day: %+v", got)
	}

	a.To = core.MustDate("2026-08-07")
	a2, err := s.UpdateAbsence(ctx, a, core.Change{Reason: "shorter"})
	must(t, err)
	_, err = s.UpdateAbsence(ctx, a, core.Change{})
	wantErr(t, err, core.ErrConflict)
	must(t, s.DeleteAbsence(ctx, "alice", a.ID, a2.Version, core.Change{}))
	revs, err := s.Revisions(ctx, "alice", a.ID)
	must(t, err)
	if len(revs) != 3 || revs[2].Op != core.OpDelete || revs[0].Type != core.RecordAbsence {
		t.Fatalf("absence history: %+v", revs)
	}
	got, err = s.Absences(ctx, core.AbsenceFilter{Owner: "alice"})
	must(t, err)
	if len(got) != 1 {
		t.Fatalf("deleted absence listed: %+v", got)
	}
}

func purge(t *testing.T, s core.Store) {
	must(t, s.Put(ctx, employer("alice", "e1")))
	held := employer("alice", "e2")
	held.Hold = true
	must(t, s.Put(ctx, held))
	must(t, s.Put(ctx, employer("bob", "b1")))
	oldE, err := s.CreateEntry(ctx, entry("alice", "e1", "2022-12-31", 60), core.Change{})
	must(t, err)
	newE, err := s.CreateEntry(ctx, entry("alice", "e1", "2023-01-01", 60), core.Change{})
	must(t, err)
	heldE, err := s.CreateEntry(ctx, entry("alice", "e2", "2020-05-05", 60), core.Change{})
	must(t, err)
	bobE, err := s.CreateEntry(ctx, entry("bob", "b1", "2020-05-05", 60), core.Change{})
	must(t, err)
	_, err = s.CreateAbsence(ctx, core.Absence{Owner: "alice", EmployerID: "e1", Type: core.AbsenceVacation,
		From: core.MustDate("2022-12-28"), To: core.MustDate("2022-12-30")}, core.Change{})
	must(t, err)
	must(t, s.Put(ctx, core.LeaveNotice{ID: "n1", Owner: "alice", EmployerID: "e1", Year: 2022, NotifiedAt: time.Now()}))
	must(t, s.Put(ctx, core.LeaveNotice{ID: "n2", Owner: "alice", EmployerID: "e1", Year: 2023, NotifiedAt: time.Now()}))

	cutoff := core.Retention{Class: core.ClassTimeRecords, YearsAfterYearEnd: 3}.Cutoff(core.MustDate("2026-11-17"))
	n, err := s.Purge(ctx, "alice", core.ClassTimeRecords, cutoff)
	must(t, err)
	if n != 2 {
		t.Fatalf("purged %d, want the old entry and the old absence", n)
	}
	for _, x := range []struct {
		owner, id string
		gone      bool
	}{{"alice", oldE.ID, true}, {"alice", newE.ID, false}, {"alice", heldE.ID, false}, {"bob", bobE.ID, false}} {
		_, err := s.Entry(ctx, x.owner, x.id)
		if x.gone != errors.Is(err, core.ErrNotFound) {
			t.Fatalf("entry %s gone=%v, err %v", x.id, x.gone, err)
		}
	}
	revs, err := s.Revisions(ctx, "alice", oldE.ID)
	must(t, err)
	if len(revs) != 0 {
		t.Fatal("the history of a purged entry stayed")
	}
	n, err = s.Purge(ctx, "alice", core.ClassLeaveNotices, cutoff)
	must(t, err)
	c, err := s.Catalog(ctx, "alice")
	must(t, err)
	if n != 1 || len(c.LeaveNotices) != 1 || c.LeaveNotices[0].Year != 2023 {
		t.Fatalf("leave notices after purge (%d): %+v", n, c.LeaveNotices)
	}
	if len(c.PurgeLogs) != 2 || c.PurgeLogs[0].Cutoff != cutoff {
		t.Fatalf("purge log: %+v", c.PurgeLogs)
	}
	_, err = s.Purge(ctx, "alice", "nonsense", cutoff)
	wantErr(t, err, core.ErrInvalid)
}

func erase(t *testing.T, s core.Store) {
	must(t, s.Put(ctx, employer("alice", "e1")))
	must(t, s.Put(ctx, employer("bob", "b1")))
	_, err := s.CreateEntry(ctx, entry("alice", "e1", "2026-03-02", 60), core.Change{})
	must(t, err)
	bobE, err := s.CreateEntry(ctx, entry("bob", "b1", "2026-03-02", 60), core.Change{})
	must(t, err)
	_, err = s.CreateAbsence(ctx, core.Absence{Owner: "alice", Type: core.AbsenceSick, From: core.MustDate("2026-03-03"),
		To: core.MustDate("2026-03-03")}, core.Change{})
	must(t, err)

	x, err := core.ExportOwner(ctx, s, "alice")
	must(t, err)
	if len(x.Entries) != 1 || len(x.Absences) != 1 || len(x.Revisions) != 2 || len(x.Catalog.Employers) != 1 {
		t.Fatalf("export: %+v", x)
	}

	must(t, s.Erase(ctx, "alice"))
	x, err = core.ExportOwner(ctx, s, "alice")
	must(t, err)
	if len(x.Entries)+len(x.Absences)+len(x.Revisions)+len(x.Catalog.Employers) != 0 {
		t.Fatalf("erase left something: %+v", x)
	}
	if _, err := s.Entry(ctx, "bob", bobE.ID); err != nil {
		t.Fatalf("erase took bob's entry: %v", err)
	}
}

func managed(owner, id, org string) core.Employer {
	e := employer(owner, id)
	e.Holder, e.Organization = core.HolderManaged, org
	return e
}

func managedSeparate(t *testing.T, s core.Store) {
	// alice works for acme (managed) and on her own account (private); bob works for acme and for globex.
	must(t, s.Put(ctx, managed("alice", "m1", "acme")))
	must(t, s.Put(ctx, employer("alice", "p1")))
	must(t, s.Put(ctx, managed("bob", "m2", "acme")))
	must(t, s.Put(ctx, managed("bob", "m3", "globex")))
	for _, id := range []string{"m1", "p1"} {
		must(t, s.Put(ctx, core.Schedule{ID: "s-" + id, Owner: "alice", EmployerID: id, ValidFrom: core.MustDate("2026-01-01")}))
	}
	must(t, s.Put(ctx, core.Holiday{ID: "h", Owner: "alice", Date: core.MustDate("2026-03-03"), Name: "Own", Fraction: 1000}))
	must(t, s.Put(ctx, core.Tag{ID: "t", Owner: "alice", Path: "private"}))
	for _, e := range []core.Entry{entry("alice", "m1", "2026-03-02", 480), entry("alice", "p1", "2026-03-02", 120),
		entry("bob", "m2", "2026-03-02", 300), entry("bob", "m3", "2026-03-02", 60)} {
		_, err := s.CreateEntry(ctx, e, core.Change{})
		must(t, err)
	}
	_, err := s.CreateAbsence(ctx, core.Absence{Owner: "alice", Type: core.AbsenceSick, From: core.MustDate("2026-03-04"),
		To: core.MustDate("2026-03-04")}, core.Change{})
	must(t, err)
	_, err = s.CreateAbsence(ctx, core.Absence{Owner: "alice", EmployerID: "m1", Type: core.AbsenceVacation,
		From: core.MustDate("2026-03-05"), To: core.MustDate("2026-03-05")}, core.Change{})
	must(t, err)
	// carol points records of her own at alice's managed employer: they must not reach acme.
	must(t, s.Put(ctx, core.Schedule{ID: "forged", Owner: "carol", EmployerID: "m1", ValidFrom: core.MustDate("2026-01-01")}))
	_, err = s.CreateEntry(ctx, entry("carol", "m1", "2026-03-02", 999), core.Change{})
	must(t, err)

	from, to := core.MustDate("2026-03-01"), core.MustDate("2026-03-31")
	acme, err := s.Managed(ctx, "acme", from, to)
	must(t, err)
	var emps []string
	for _, e := range acme.Catalog.Employers {
		emps = append(emps, e.Owner+"/"+e.ID)
	}
	if len(emps) != 2 || !contains(emps, "alice/m1") || !contains(emps, "bob/m2") {
		t.Fatalf("acme sees employers %v", emps)
	}
	if len(acme.Catalog.Schedules) != 1 || acme.Catalog.Schedules[0].ID != "s-m1" || len(acme.Catalog.Holidays) != 0 ||
		len(acme.Catalog.Tags) != 0 {
		t.Fatalf("acme sees records beyond its employers: %+v", acme.Catalog)
	}
	minutes := 0
	for _, e := range acme.Entries {
		if e.EmployerID != "m1" && e.EmployerID != "m2" || e.Owner == "carol" {
			t.Fatalf("acme sees entry %+v", e)
		}
		minutes += e.Minutes
	}
	if minutes != 780 || len(acme.Absences) != 1 || acme.Absences[0].Type != core.AbsenceVacation {
		t.Fatalf("acme: %d minutes, absences %+v", minutes, acme.Absences)
	}
	globex, err := s.Managed(ctx, "globex", from, to)
	must(t, err)
	if len(globex.Catalog.Employers) != 1 || len(globex.Entries) != 1 || globex.Entries[0].Minutes != 60 {
		t.Fatalf("globex: %+v", globex)
	}
	_, err = s.Managed(ctx, "", from, to)
	wantErr(t, err, core.ErrInvalid)

	// alice leaves: her private records go, acme keeps its own.
	must(t, s.Erase(ctx, "alice"))
	c, err := s.Catalog(ctx, "alice")
	must(t, err)
	if len(c.Employers) != 1 || c.Employers[0].ID != "m1" || len(c.Schedules) != 1 || len(c.Tags) != 0 || len(c.Holidays) != 0 {
		t.Fatalf("after erase alice has %+v", c)
	}
	left, err := s.Entries(ctx, core.EntryFilter{Owner: "alice", IncludeDeleted: true})
	must(t, err)
	abs, err := s.Absences(ctx, core.AbsenceFilter{Owner: "alice"})
	must(t, err)
	revs, err := s.Revisions(ctx, "alice", "")
	must(t, err)
	if len(left) != 1 || left[0].EmployerID != "m1" || len(abs) != 1 || abs[0].EmployerID != "m1" || len(revs) != 2 {
		t.Fatalf("after erase: entries %+v absences %+v revisions %d", left, abs, len(revs))
	}
	again, err := s.Managed(ctx, "acme", from, to)
	must(t, err)
	if len(again.Entries) != len(acme.Entries) || len(again.Absences) != 1 {
		t.Fatal("erase took records from acme")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func organizations(t *testing.T, s core.Store) {
	_, err := s.Organization(ctx, "acme")
	wantErr(t, err, core.ErrNotFound)
	org := core.Organization{ID: "acme", Name: "ACME", Region: "DE-BY", Templates: []core.ScheduleTemplate{
		{Name: "Full time", Mode: core.ModePlan, LeaveDays: 30}}}
	org.Templates[0].Days[time.Monday].Minutes = 480
	must(t, s.PutOrganization(ctx, org))
	// Company features need the agreement.
	bad := org
	bad.Features.SupervisorView = true
	wantErr(t, s.PutOrganization(ctx, bad), core.ErrInvalid)

	m, emp, sched, err := core.Join(org, "alice", core.RoleMember, "Full time", core.MustDate("2026-01-01"))
	must(t, err)
	must(t, s.PutMembership(ctx, m))
	must(t, s.Put(ctx, emp))
	must(t, s.Put(ctx, *sched))
	admin := core.Membership{ID: "adm", Organization: "acme", Person: "boss", Role: core.RoleAdmin, From: core.MustDate("2026-01-01")}
	must(t, s.PutMembership(ctx, admin))
	wantErr(t, s.PutMembership(ctx, core.Membership{ID: "x", Organization: "nobody", Person: "boss", Role: core.RoleAdmin,
		From: core.MustDate("2026-01-01")}), core.ErrNotFound)
	stolen := admin
	stolen.Person = "mallory"
	wantErr(t, s.PutMembership(ctx, stolen), core.ErrConflict)

	got, err := s.Organization(ctx, "acme")
	must(t, err)
	if got.Name != "ACME" || len(got.Templates) != 1 || got.Templates[0].Days[time.Monday].Minutes != 480 {
		t.Fatalf("organization: %+v", got)
	}
	all, err := s.Memberships(ctx, "acme", "")
	must(t, err)
	mine, err := s.Memberships(ctx, "", "alice")
	must(t, err)
	if len(all) != 2 || len(mine) != 1 || mine[0].EmployerID != emp.ID {
		t.Fatalf("memberships: %+v / %+v", all, mine)
	}
	actor := core.ActorFor("boss", "acme", all, core.MustDate("2026-06-01"))
	if !core.MayChangeSettings(emp, actor) || core.MayBook(emp, actor) {
		t.Fatalf("admin rights: %+v", actor)
	}
	data, err := s.Managed(ctx, "acme", core.MustDate("2026-01-01"), core.MustDate("2026-12-31"))
	must(t, err)
	if len(data.Catalog.Employers) != 1 || len(data.Catalog.Schedules) != 1 {
		t.Fatalf("acme manages %+v", data.Catalog)
	}

	// Leaving: the membership ends, the records stay with the organization, alice's erasure does not reach them.
	ended, archived, err := core.EndMembership(m, emp, core.MustDate("2026-06-30"))
	must(t, err)
	must(t, s.PutMembership(ctx, ended))
	must(t, s.Put(ctx, archived))
	must(t, s.Erase(ctx, "alice"))
	wantErr(t, s.Erase(ctx, ""), core.ErrInvalid)
	if _, err := s.Organization(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	data, err = s.Managed(ctx, "acme", core.MustDate("2026-01-01"), core.MustDate("2026-12-31"))
	must(t, err)
	if len(data.Catalog.Employers) != 1 || !data.Catalog.Employers[0].Archived || len(data.Catalog.Schedules) != 1 {
		t.Fatalf("after alice left, acme has %+v", data.Catalog)
	}
	mine, err = s.Memberships(ctx, "", "alice")
	must(t, err)
	if len(mine) != 1 || mine[0].Until != core.MustDate("2026-06-30") || core.ActorFor("alice", "acme", mine, core.MustDate("2026-07-01")).Organization != "" {
		t.Fatalf("ended membership: %+v", mine)
	}
}
