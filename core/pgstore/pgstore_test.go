// SPDX-License-Identifier: MIT

package pgstore_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/core/pgstore"
	"github.com/Ollornog/EightPerDay/core/storetest"
	"github.com/Ollornog/EightPerDay/internal/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }

var ctx = context.Background()

// The role the app works as: no superuser, no BYPASSRLS, so row-level security holds for it.
const appRole = "epd_test_app"

// database migrates a fresh database as its owner and returns its address.
func database(t *testing.T, o pgstore.Options) string {
	t.Helper()
	url := pgtest.URL(t)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if err := pgstore.Migrate(ctx, conn, o); err != nil {
		t.Fatal(err)
	}
	// A second run changes nothing.
	if err := pgstore.Migrate(ctx, conn, o); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
	_, err = conn.Exec(ctx, `DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '`+appRole+`') THEN
			CREATE ROLE `+appRole+` NOLOGIN NOSUPERUSER NOBYPASSRLS;
		END IF;
	END $$;
	GRANT USAGE ON SCHEMA public TO `+appRole+`;
	GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO `+appRole)
	if err != nil {
		t.Fatal(err)
	}
	return url
}

// pool connects as the app role with the tenant set, as an embedding application would.
func pool(t *testing.T, url, setting, tenant string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET ROLE "+appRole); err != nil {
			return err
		}
		if tenant == "" {
			return nil
		}
		_, err := conn.Exec(ctx, "SELECT set_config($1, $2, false)", setting, tenant)
		return err
	}
	p, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func store(t *testing.T, db pgstore.DB, o pgstore.Options) *pgstore.Store {
	t.Helper()
	s, err := pgstore.New(db, o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestConformance(t *testing.T) {
	o := pgstore.Options{Prefix: "time_", TenantSetting: "app.tenant"}
	storetest.Run(t, func(t *testing.T) core.Store {
		return store(t, pool(t, database(t, o), o.TenantSetting, "main"), o)
	})
}

// No tenant can read, find, change, purge or erase anything of another, and a connection without a tenant sees and
// writes nothing.
func TestTenantsAreSeparate(t *testing.T) {
	o := pgstore.Options{TenantSetting: "underdoc.tenant"}
	url := database(t, o)
	a := store(t, pool(t, url, o.TenantSetting, "acme"), o)
	b := store(t, pool(t, url, o.TenantSetting, "globex"), o)
	none := store(t, pool(t, url, o.TenantSetting, ""), o)

	emp := core.Employer{ID: "e1", Owner: "alice", Name: "Example", Kind: core.KindEmployer, Mode: core.ModeExact}
	if err := a.Put(ctx, emp); err != nil {
		t.Fatal(err)
	}
	e, err := a.CreateEntry(ctx, core.Entry{ID: "x1", Owner: "alice", EmployerID: "e1", Kind: core.EntryWork,
		Date: core.MustDate("2020-01-02"), Minutes: 60}, core.Change{})
	if err != nil {
		t.Fatal(err)
	}

	// Same owner name and same IDs in another tenant: separate rows, no collision.
	if err := b.Put(ctx, emp); err != nil {
		t.Fatalf("same ID in another tenant: %v", err)
	}
	cat, err := b.Catalog(ctx, "alice")
	if err != nil || len(cat.Employers) != 1 {
		t.Fatalf("tenant b catalog: %v %+v", err, cat)
	}
	if _, err := b.Entry(ctx, "alice", e.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("tenant b reads tenant a's entry: %v", err)
	}
	list, _ := b.Entries(ctx, core.EntryFilter{Owner: "alice", IncludeDeleted: true})
	revs, _ := b.Revisions(ctx, "alice", "")
	if len(list) != 0 || len(revs) != 0 {
		t.Fatal("tenant b lists tenant a's entries or history")
	}
	upd := e
	upd.Minutes = 1
	if _, err := b.UpdateEntry(ctx, upd, core.Change{}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("tenant b changes tenant a's entry: %v", err)
	}
	if err := b.DeleteEntry(ctx, "alice", e.ID, 1, core.Change{}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("tenant b deletes tenant a's entry: %v", err)
	}
	if n, err := b.Purge(ctx, "", core.ClassTimeRecords, core.MustDate("2030-01-01")); err != nil || n != 0 {
		t.Fatalf("tenant b purges tenant a: %d %v", n, err)
	}
	if err := b.Erase(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if got, err := a.Entry(ctx, "alice", e.ID); err != nil || got.Minutes != 60 {
		t.Fatalf("tenant a's entry after tenant b's attempts: %+v %v", got, err)
	}
	if cat, _ := a.Catalog(ctx, "alice"); len(cat.Employers) != 1 {
		t.Fatal("tenant b's erase reached tenant a")
	}

	// Without a tenant: nothing to see, nothing to write.
	cat, err = none.Catalog(ctx, "alice")
	if err != nil || len(cat.Employers) != 0 {
		t.Fatalf("no tenant sees: %v %+v", err, cat)
	}
	if err := none.Put(ctx, core.Tag{ID: "t1", Owner: "alice", Path: "x"}); err == nil {
		t.Fatal("a connection without tenant wrote a row")
	}
}

func TestOptions(t *testing.T) {
	for _, o := range []pgstore.Options{
		{Prefix: "Bad"}, {Prefix: "x; DROP TABLE y; --"}, {Prefix: "1x_"},
		{TenantSetting: "notenant"}, {TenantSetting: "a.b'); DROP TABLE x; --"},
	} {
		if _, err := pgstore.Migrations(o); err == nil {
			t.Errorf("options %+v accepted", o)
		}
		if _, err := pgstore.New(nil, o); err == nil {
			t.Errorf("New accepted %+v", o)
		}
	}
	steps, err := pgstore.Migrations(pgstore.Options{Prefix: "time_", TenantSetting: "underdoc.tenant"})
	if err != nil || len(steps) == 0 || steps[0].Version != 1 {
		t.Fatalf("migrations: %v %+v", err, steps)
	}
	for _, m := range steps {
		if strings.Contains(m.SQL, "{{") || !strings.Contains(m.SQL, "CREATE TABLE time_") ||
			!strings.Contains(m.SQL, "current_setting('underdoc.tenant', true)") {
			t.Fatalf("migration %d not rendered: %s", m.Version, m.SQL[:200])
		}
	}
}

// An embedding application runs the rendered SQL in its own migration flow; the store works on that schema.
func TestEmbeddedMigrationFlow(t *testing.T) {
	o := pgstore.Options{Prefix: "ud_time_", TenantSetting: "underdoc.tenant"}
	url := pgtest.URL(t)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	steps, err := pgstore.Migrations(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range steps {
		if _, err := conn.Exec(ctx, m.SQL); err != nil {
			t.Fatalf("migration %d: %v", m.Version, err)
		}
	}
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE tablename LIKE 'ud_time_%' AND rowsecurity").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("%d tables with row-level security, want 5", n)
	}
	if _, err := conn.Exec(ctx, "SELECT set_config('underdoc.tenant', 'main', false)"); err != nil {
		t.Fatal(err)
	}
	s := store(t, conn, o)
	if err := s.Put(ctx, core.Employer{ID: "e1", Owner: "a", Name: "X", Kind: core.KindPrivate, Mode: core.ModeWeekSum}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEntry(ctx, core.Entry{Owner: "a", EmployerID: "e1", Kind: core.EntryWork,
		Date: core.MustDate("2026-11-16"), Minutes: 38*60 + 30, Precision: core.PrecisionWeek}, core.Change{}); err != nil {
		t.Fatal(err)
	}
}

func TestConnect(t *testing.T) {
	o := pgstore.Options{}
	url := database(t, o)
	if _, err := pgstore.Connect(ctx, url, o, ""); err == nil {
		t.Fatal("Connect without tenant")
	}
	p, err := pgstore.Connect(ctx, url, o, "main")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s := store(t, p, o)
	if err := s.Put(ctx, core.Tag{ID: "t1", Owner: "a", Path: "x"}); err != nil {
		t.Fatal(err)
	}
	var tenant string
	if err := p.QueryRow(ctx, "SELECT tenant FROM epd_records WHERE id = 't1'").Scan(&tenant); err != nil || tenant != "main" {
		t.Fatalf("tenant %q %v", tenant, err)
	}
}

func TestAccessLog(t *testing.T) {
	o := pgstore.Options{Prefix: "time_", TenantSetting: "app.tenant"}
	storetest.RunAccessLog(t, func(t *testing.T) storetest.AccessLogStore {
		return store(t, pool(t, database(t, o), o.TenantSetting, "main"), o)
	})
}

// The access log only grows: the app role can neither change a row nor delete a young one, and tenants are separate.
func TestAccessLogOnlyGrows(t *testing.T) {
	o := pgstore.Options{TenantSetting: "eightperday.tenant"}
	url := database(t, o)
	p := pool(t, url, o.TenantSetting, "acme")
	a := store(t, p, o)
	b := store(t, pool(t, url, o.TenantSetting, "globex"), o)
	e, err := a.LogAccess(ctx, core.Access{Actor: "boss", Subject: "alice", Action: core.AccessRead, What: "entries"})
	if err != nil {
		t.Fatal(err)
	}
	if tag, err := p.Exec(ctx, "UPDATE epd_access_log SET doc = jsonb_set(doc, '{what}', '\"nothing\"')"); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("update: %v rows, %v", tag.RowsAffected(), err)
	}
	if tag, err := p.Exec(ctx, "DELETE FROM epd_access_log"); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("delete: %v rows, %v", tag.RowsAffected(), err)
	}
	log, err := a.Accesses(ctx, "alice")
	if err != nil || len(log) != 1 || log[0].Hash != e.Hash || log[0].What != "entries" {
		t.Fatalf("after the attempts: %+v %v", log, err)
	}
	if other, err := b.Accesses(ctx, "alice"); err != nil || len(other) != 0 {
		t.Fatalf("tenant globex reads acme's log: %+v %v", other, err)
	}
	// The other tenant starts a chain of its own.
	if x, err := b.LogAccess(ctx, core.Access{Actor: "boss", Subject: "alice", Action: core.AccessRead, What: "entries"}); err != nil || x.Prev != "" {
		t.Fatalf("globex chain: %+v %v", x, err)
	}
}
