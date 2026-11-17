// SPDX-License-Identifier: MIT

package pgstore

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Options are chosen by the application. They are part of the schema: keep them once the migrations ran.
type Options struct {
	// Prefix goes in front of every table name, such as "epd_" (the default) or "time_".
	Prefix string
	// TenantSetting is the setting that carries the tenant of a connection, such as "eightperday.tenant" (the
	// default) or "underdoc.tenant". Row-level security compares the tenant column with it.
	TenantSetting string
}

var (
	prefixPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)
	settingPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,40}\.[a-z_][a-z0-9_]{0,40}$`)
)

func (o Options) withDefaults() (Options, error) {
	if o.Prefix == "" {
		o.Prefix = "epd_"
	}
	if o.TenantSetting == "" {
		o.TenantSetting = "eightperday.tenant"
	}
	if !prefixPattern.MatchString(o.Prefix) {
		return o, fmt.Errorf("pgstore: table prefix %q: lower-case letters, digits and _, starting with a letter", o.Prefix)
	}
	if !settingPattern.MatchString(o.TenantSetting) {
		return o, fmt.Errorf("pgstore: tenant setting %q: want a custom setting like app.tenant", o.TenantSetting)
	}
	return o, nil
}

// Migration is one step of the schema, as plain SQL for the options it was rendered with.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations renders the migrations for the options, in order. An application with its own migration flow runs
// them there, each once, in a transaction; Migrate does the same with a table of its own.
func Migrations(o Options) ([]Migration, error) {
	o, err := o.withDefaults()
	if err != nil {
		return nil, err
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	var out []Migration
	for _, n := range names {
		base := strings.TrimSuffix(strings.TrimPrefix(n, "migrations/"), ".sql")
		num, name, ok := strings.Cut(base, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil {
			return nil, fmt.Errorf("pgstore: migration file %q: want NNN_name.sql", n)
		}
		raw, err := migrationFiles.ReadFile(n)
		if err != nil {
			return nil, err
		}
		sql := strings.NewReplacer("{{prefix}}", o.Prefix, "{{tenant_setting}}", o.TenantSetting).Replace(string(raw))
		out = append(out, Migration{Version: v, Name: name, SQL: sql})
	}
	return out, nil
}

// Migrate brings the schema up to date for a standalone program. It keeps the applied versions in the table
// <prefix>schema_migrations (shared by all tenants, it holds no data) and serialises concurrent runs with an advisory
// lock. Run it as the role that owns the tables.
func Migrate(ctx context.Context, db DB, o Options) error {
	o, err := o.withDefaults()
	if err != nil {
		return err
	}
	steps, err := Migrations(o)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", o.Prefix+"schema_migrations"); err != nil {
		return err
	}
	table := o.Prefix + "schema_migrations"
	if _, err := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+table+
		" (version integer PRIMARY KEY, name text NOT NULL, applied timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT version FROM "+table)
	if err != nil {
		return err
	}
	done := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		done[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range steps {
		if done[m.Version] {
			continue
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("pgstore: migration %03d_%s: %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO "+table+" (version, name) VALUES ($1, $2)", m.Version, m.Name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
