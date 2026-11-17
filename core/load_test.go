// SPDX-License-Identifier: MIT

package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/core/memstore"
)

// Load reaches one day back for entries, so the part of a night shift after midnight counts on the first day.
func TestLoadReachesBackForNightShifts(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	emp := core.Employer{ID: "e1", Owner: "alice", Name: "Night GmbH", Kind: core.KindEmployer, Mode: core.ModeExact,
		DaySplit: core.SplitMidnight}
	if err := s.Put(ctx, emp); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Europe/Berlin")
	start := time.Date(2026, 3, 1, 22, 0, 0, 0, loc)
	end := start.Add(8 * time.Hour)
	if _, err := s.CreateEntry(ctx, core.Entry{Owner: "alice", EmployerID: "e1", Kind: core.EntryWork, Start: &start,
		End: &end, TimeZone: "Europe/Berlin"}, core.Change{}); err != nil {
		t.Fatal(err)
	}
	data, err := core.Load(ctx, s, "alice", core.MustDate("2026-03-02"), core.MustDate("2026-03-02"))
	if err != nil {
		t.Fatal(err)
	}
	days, err := core.Days(data, "e1", core.MustDate("2026-03-02"), core.MustDate("2026-03-02"))
	if err != nil || days[0].Worked != 360 {
		t.Fatalf("the night's part on 2 March: %+v %v", days, err)
	}
}

// Every read of an organization is logged for each person whose records it gets, and only for them.
func TestReadManagedLogs(t *testing.T) {
	ctx := context.Background()
	s := memstore.New()
	for _, e := range []core.Employer{
		{ID: "m1", Owner: "alice", Name: "ACME", Kind: core.KindEmployer, Mode: core.ModeExact, Holder: core.HolderManaged, Organization: "acme"},
		{ID: "m2", Owner: "bob", Name: "ACME", Kind: core.KindEmployer, Mode: core.ModeExact, Holder: core.HolderManaged, Organization: "acme"},
		{ID: "p1", Owner: "carol", Name: "Own", Kind: core.KindPrivate, Mode: core.ModeExact},
	} {
		if err := s.Put(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	boss := core.Actor{Person: "boss", Organization: "acme", Admin: true}
	d, err := core.ReadManaged(ctx, s, s, boss, "acme", core.MustDate("2026-03-01"), core.MustDate("2026-03-31"), "payroll March")
	if err != nil || len(d.Catalog.Employers) != 2 {
		t.Fatalf("read: %+v %v", d, err)
	}
	for subject, want := range map[string]int{"alice": 1, "bob": 1, "carol": 0} {
		log, err := s.Accesses(ctx, subject)
		if err != nil || len(log) != want {
			t.Fatalf("%s: %+v %v", subject, log, err)
		}
		if want == 1 && (log[0].Actor != "boss" || log[0].Action != core.AccessRead || log[0].What != "payroll March" ||
			log[0].From != core.MustDate("2026-03-01")) {
			t.Fatalf("%s: %+v", subject, log[0])
		}
	}
}
