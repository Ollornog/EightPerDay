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
