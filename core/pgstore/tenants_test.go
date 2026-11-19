// SPDX-License-Identifier: MIT

package pgstore_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/core/pgstore"
	"github.com/Ollornog/EightPerDay/core/storetest"
)

// TestNoTenantReachesAnother is the proof of M-3: through no method of the store does one tenant read or change rows
// of another, and a connection without a tenant reads and writes nothing. It covers every exported method of
// pgstore.Store; a method added later fails the test until it is covered here.
func TestNoTenantReachesAnother(t *testing.T) {
	o := pgstore.Options{Prefix: "time_", TenantSetting: "underdoc.tenant"}
	url := database(t, o)
	a := store(t, pool(t, url, o.TenantSetting, "acme"), o)
	b := store(t, pool(t, url, o.TenantSetting, "globex"), o)
	none := store(t, pool(t, url, o.TenantSetting, ""), o)

	// Tenant acme: a private and a managed employer of alice, an organization with her membership, entries and
	// absences with their history, an access log entry and a running stopwatch.
	old := core.MustDate("2020-01-06")
	year := [2]core.Date{core.MustDate("2020-01-01"), core.MustDate("2030-12-31")}
	org := core.Organization{ID: "org", Name: "Acme"}
	private := core.Employer{ID: "e1", Owner: "alice", Name: "Own", Kind: core.KindPrivate, Mode: core.ModeExact}
	managed := core.Employer{ID: "m1", Owner: "alice", Name: "Acme", Kind: core.KindEmployer, Holder: core.HolderManaged,
		Organization: "org", Mode: core.ModeSchedule}
	member := core.Membership{ID: "ms1", Organization: "org", Person: "alice", Role: core.RoleMember, EmployerID: "m1",
		From: old}
	for _, err := range []error{a.PutOrganization(ctx, org), a.Put(ctx, private), a.Put(ctx, managed),
		a.PutMembership(ctx, member), a.Put(ctx, core.Tag{ID: "t1", Owner: "alice", Path: "site"})} {
		if err != nil {
			t.Fatal(err)
		}
	}
	entry := core.Entry{ID: "x1", Owner: "alice", EmployerID: "e1", Kind: core.EntryWork, Date: old, Minutes: 60}
	if _, err := a.CreateEntry(ctx, entry, core.Change{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateEntry(ctx, core.Entry{ID: "x2", Owner: "alice", EmployerID: "m1", Kind: core.EntryWork, Date: old,
		Minutes: 480}, core.Change{}); err != nil {
		t.Fatal(err)
	}
	absence := core.Absence{ID: "y1", Owner: "alice", EmployerID: "m1", Type: core.AbsenceVacation, From: old, To: old,
		Status: core.StatusApproved}
	if _, err := a.CreateAbsence(ctx, absence, core.Change{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LogAccess(ctx, core.Access{At: time.Now().AddDate(-5, 0, 0), Actor: "boss", Organization: "org",
		Subject: "alice", Action: core.AccessRead, What: "entries"}); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 18, 7, 0, 0, 0, time.UTC)
	if err := a.StartTimer(ctx, "alice", core.Timer{EmployerID: "e1", Started: started}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var parts []any
		cat, err := a.Catalog(ctx, "alice")
		parts = append(parts, cat, err)
		entries, err := a.Entries(ctx, core.EntryFilter{Owner: "alice", IncludeDeleted: true})
		parts = append(parts, entries, err)
		absences, err := a.Absences(ctx, core.AbsenceFilter{Owner: "alice", IncludeDeleted: true})
		parts = append(parts, absences, err)
		revisions, err := a.Revisions(ctx, "alice", "")
		parts = append(parts, revisions, err)
		o, err := a.Organization(ctx, "org")
		parts = append(parts, o, err)
		ms, err := a.Memberships(ctx, "org", "")
		parts = append(parts, ms, err)
		m, err := a.Managed(ctx, "org", year[0], year[1])
		parts = append(parts, m, err)
		log, err := a.Accesses(ctx, "alice")
		parts = append(parts, log, err)
		timer, err := a.RunningTimer(ctx, "alice")
		parts = append(parts, timer, err)
		out, err := json.Marshal(parts)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	before := snapshot()
	if len(before) < 1000 {
		t.Fatalf("tenant acme holds too little to prove anything: %s", before)
	}

	covered := map[string]bool{}
	check := func(method string, err error) {
		t.Helper()
		covered[method] = true
		if err != nil {
			t.Errorf("%s: %v", method, err)
		}
	}
	notFound := func(err error) error {
		if errors.Is(err, core.ErrNotFound) {
			return nil
		}
		return errors.New("want not found, got " + errString(err))
	}
	empty := func(n int, err error) error {
		if err != nil {
			return err
		}
		if n != 0 {
			return errors.New("sees rows of tenant acme")
		}
		return nil
	}

	// Reading: another tenant and no tenant see nothing of acme.
	for name, s := range map[string]*pgstore.Store{"globex": b, "no tenant": none} {
		t.Run("read as "+name, func(t *testing.T) {
			cat, err := s.Catalog(ctx, "alice")
			check("Catalog", empty(len(cat.Employers)+len(cat.Tags), err))
			_, err = s.Entry(ctx, "alice", "x1")
			check("Entry", notFound(err))
			entries, err := s.Entries(ctx, core.EntryFilter{Owner: "alice", IncludeDeleted: true})
			check("Entries", empty(len(entries), err))
			absences, err := s.Absences(ctx, core.AbsenceFilter{Owner: "alice", IncludeDeleted: true})
			check("Absences", empty(len(absences), err))
			revisions, err := s.Revisions(ctx, "alice", "")
			check("Revisions", empty(len(revisions), err))
			_, err = s.Organization(ctx, "org")
			check("Organization", notFound(err))
			ms, err := s.Memberships(ctx, "", "alice")
			check("Memberships", empty(len(ms), err))
			m, err := s.Managed(ctx, "org", year[0], year[1])
			check("Managed", empty(len(m.Catalog.Employers)+len(m.Entries)+len(m.Absences), err))
			log, err := s.Accesses(ctx, "alice")
			check("Accesses", empty(len(log), err))
			timer, err := s.RunningTimer(ctx, "alice")
			if timer != nil {
				err = errors.New("sees the stopwatch of tenant acme")
			}
			check("RunningTimer", err)
		})
	}

	// Changing: what names acme's rows finds nothing, and the bulk operations stay in their own tenant.
	upd := entry
	upd.Version, upd.Minutes = 1, 1
	_, err := b.UpdateEntry(ctx, upd, core.Change{})
	check("UpdateEntry", notFound(err))
	check("DeleteEntry", notFound(b.DeleteEntry(ctx, "alice", "x1", 1, core.Change{})))
	updAbsence := absence
	updAbsence.Version, updAbsence.Type = 1, core.AbsenceSick
	_, err = b.UpdateAbsence(ctx, updAbsence, core.Change{})
	check("UpdateAbsence", notFound(err))
	check("DeleteAbsence", notFound(b.DeleteAbsence(ctx, "alice", "y1", 1, core.Change{})))
	check("Delete", notFound(b.Delete(ctx, core.RecTag, "alice", "t1")))
	stopped, err := b.StopTimer(ctx, "alice")
	if err == nil && stopped != nil {
		err = errors.New("stopped the stopwatch of tenant acme")
	}
	check("StopTimer", err)
	total := 0
	for _, class := range []core.Class{core.ClassTimeRecords, core.ClassLeaveNotices, core.ClassAccessLog} {
		n, err := b.Purge(ctx, "", class, core.MustDate("2030-01-01"))
		if err != nil {
			check("Purge", err)
		}
		total += n
	}
	check("Purge", empty(total, nil))
	check("Erase", b.Erase(ctx, "alice"))

	// Writing the same IDs in globex makes rows of its own: no collision, and acme's rows stay as they were.
	check("PutOrganization", b.PutOrganization(ctx, core.Organization{ID: "org", Name: "Globex"}))
	globexEmployer := private
	globexEmployer.Name = "Taken over"
	check("Put", b.Put(ctx, globexEmployer))
	globexMember := member
	globexMember.Role = core.RoleAdmin
	check("PutMembership", b.PutMembership(ctx, globexMember))
	_, err = b.CreateEntry(ctx, entry, core.Change{})
	check("CreateEntry", err)
	_, err = b.CreateAbsence(ctx, absence, core.Change{})
	check("CreateAbsence", err)
	x, err := b.LogAccess(ctx, core.Access{Actor: "boss", Subject: "alice", Action: core.AccessRead, What: "entries"})
	if err == nil && x.Prev != "" {
		err = errors.New("the access log of globex continues the chain of acme")
	}
	check("LogAccess", err)
	check("StartTimer", b.StartTimer(ctx, "alice", core.Timer{EmployerID: "e1", Started: started.Add(time.Hour)}))
	if cat, err := b.Catalog(ctx, "alice"); err != nil || len(cat.Employers) != 1 || cat.Employers[0].Name != "Taken over" {
		t.Errorf("globex sees its own employer only: %+v %v", cat.Employers, err)
	}

	// No tenant writes nothing.
	if err := none.Put(ctx, core.Tag{ID: "t2", Owner: "alice", Path: "x"}); err == nil {
		t.Error("a connection without a tenant wrote a catalog record")
	}
	if err := none.StartTimer(ctx, "carol", core.Timer{EmployerID: "e1", Started: started}); err == nil {
		t.Error("a connection without a tenant started a stopwatch")
	}

	if after := snapshot(); after != before {
		t.Errorf("tenant acme changed through globex:\nbefore %s\nafter  %s", before, after)
	}

	var missing []string
	st := reflect.TypeOf(a)
	for i := range st.NumMethod() {
		if name := st.Method(i).Name; !covered[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("methods of the store this test does not cover: %v", missing)
	}
	if len(covered) < 25 {
		t.Errorf("only %d methods covered", len(covered))
	}
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

// The deletion of a whole tenant reaches its access log only with the erase setting, and only for that tenant.
func TestEraseSettingDeletesATenantsAccessLog(t *testing.T) {
	o := pgstore.Options{Prefix: "time_", TenantSetting: "underdoc.tenant"}
	setting, err := o.EraseSetting()
	if err != nil || setting != "underdoc.tenant_erase" {
		t.Fatalf("erase setting %q %v", setting, err)
	}
	if _, err := (pgstore.Options{Prefix: "Bad"}).EraseSetting(); err == nil {
		t.Fatal("bad options accepted")
	}
	url := database(t, o)
	p := pool(t, url, o.TenantSetting, "acme")
	a := store(t, p, o)
	b := store(t, pool(t, url, o.TenantSetting, "globex"), o)
	for _, s := range []*pgstore.Store{a, b} {
		if _, err := s.LogAccess(ctx, core.Access{Actor: "boss", Subject: "alice", Action: core.AccessRead, What: "entries"}); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		var n int
		if err := p.QueryRow(ctx, "SELECT count(*) FROM time_access_log").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A failure below must give the connection back, or closing the pool waits for it.
	defer func() { _ = tx.Rollback(ctx) }()
	// Set to another tenant, it lifts nothing.
	if _, err := tx.Exec(ctx, "SELECT set_config($1, 'globex', true)", setting); err != nil {
		t.Fatal(err)
	}
	if tag, err := tx.Exec(ctx, "DELETE FROM time_access_log"); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("erase setting of another tenant: %d %v", tag.RowsAffected(), err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config($1, 'acme', true)", setting); err != nil {
		t.Fatal(err)
	}
	if tag, err := tx.Exec(ctx, "UPDATE time_access_log SET hash = 'x'"); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("the erase setting lets rows change: %d %v", tag.RowsAffected(), err)
	}
	if tag, err := tx.Exec(ctx, "DELETE FROM time_access_log"); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("erase of acme: %d %v", tag.RowsAffected(), err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 0 {
		t.Fatalf("acme keeps %d rows", n)
	}
	if log, err := b.Accesses(ctx, "alice"); err != nil || len(log) != 1 {
		t.Fatalf("globex lost its log: %+v %v", log, err)
	}
	// Outside that transaction the rule holds again.
	if _, err := a.LogAccess(ctx, core.Access{Actor: "boss", Subject: "alice", Action: core.AccessRead, What: "entries"}); err != nil {
		t.Fatal(err)
	}
	if tag, err := p.Exec(ctx, "DELETE FROM time_access_log"); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("delete without the setting: %d %v", tag.RowsAffected(), err)
	}
}

func TestStopwatches(t *testing.T) {
	o := pgstore.Options{Prefix: "time_", TenantSetting: "app.tenant"}
	storetest.RunStopwatches(t, func(t *testing.T) storetest.StopwatchStore {
		return store(t, pool(t, database(t, o), o.TenantSetting, "main"), o)
	})
}
