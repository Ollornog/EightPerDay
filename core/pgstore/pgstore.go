// SPDX-License-Identifier: MIT

// Package pgstore keeps the data of EightPerDay in PostgreSQL.
//
// The schema comes as embedded migrations, rendered with a table prefix and the name of the setting that carries the
// tenant (Options). Every table has a tenant column filled from that setting, and row-level security keeps each
// tenant to its own rows. The store never names a tenant: the connections it gets decide it. An embedding application
// passes its own pool, whose connections already carry the setting (and run as a role that row-level security holds
// for); a standalone program uses Connect.
package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ollornog/EightPerDay/core"
)

// DB is what the store needs from a connection: *pgxpool.Pool, *pgx.Conn and pgx.Tx all fit.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Store is a core.Store on PostgreSQL.
type Store struct {
	db                                    DB
	records, entries, absences, revisions string
}

var _ core.Store = (*Store)(nil)

// New returns a store on db. The migrations must have run with the same options.
func New(db DB, o Options) (*Store, error) {
	o, err := o.withDefaults()
	if err != nil {
		return nil, err
	}
	return &Store{db: db, records: o.Prefix + "records", entries: o.Prefix + "entries",
		absences: o.Prefix + "absences", revisions: o.Prefix + "revisions"}, nil
}

// Connect opens a pool for a standalone program whose connections all belong to one tenant.
func Connect(ctx context.Context, url string, o Options, tenant string) (*pgxpool.Pool, error) {
	o, err := o.withDefaults()
	if err != nil {
		return nil, err
	}
	if tenant == "" {
		return nil, fmt.Errorf("pgstore: no tenant")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SELECT set_config($1, $2, false)", o.TenantSetting, tenant)
		return err
	}
	return pgxpool.NewWithConfig(ctx, config)
}

func nullDate(d core.Date) any {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// Put inserts or replaces a catalog record.
func (s *Store) Put(ctx context.Context, r core.Record) error {
	if err := r.Validate(); err != nil {
		return err
	}
	m := r.Meta()
	doc, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var tag pgconn.CommandTag
	if m.Kind == core.RecPeriodClose || m.Kind == core.RecPurgeLog {
		tag, err = s.db.Exec(ctx, "INSERT INTO "+s.records+` (kind, id, owner, employer_id, doc) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (tenant, kind, id) DO NOTHING`, m.Kind, m.ID, m.Owner, m.EmployerID, doc)
		if err == nil && tag.RowsAffected() == 0 {
			return core.ErrLocked
		}
		return err
	}
	tag, err = s.db.Exec(ctx, "INSERT INTO "+s.records+` AS r (kind, id, owner, employer_id, doc) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant, kind, id) DO UPDATE SET employer_id = EXCLUDED.employer_id, doc = EXCLUDED.doc
		WHERE r.owner = EXCLUDED.owner`, m.Kind, m.ID, m.Owner, m.EmployerID, doc)
	if err == nil && tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return err
}

// Delete removes a catalog record.
func (s *Store) Delete(ctx context.Context, kind core.Kind, owner, id string) error {
	if kind == core.RecPeriodClose || kind == core.RecPurgeLog {
		var n int
		if err := s.db.QueryRow(ctx, "SELECT count(*) FROM "+s.records+" WHERE kind = $1 AND owner = $2 AND id = $3",
			kind, owner, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return core.ErrNotFound
		}
		return core.ErrLocked
	}
	tag, err := s.db.Exec(ctx, "DELETE FROM "+s.records+" WHERE kind = $1 AND owner = $2 AND id = $3", kind, owner, id)
	if err == nil && tag.RowsAffected() == 0 {
		return core.ErrNotFound
	}
	return err
}

// Catalog returns every catalog record of the owner.
func (s *Store) Catalog(ctx context.Context, owner string) (core.Catalog, error) {
	return s.catalog(ctx, s.db, owner)
}

func (s *Store) catalog(ctx context.Context, db DB, owner string) (core.Catalog, error) {
	rows, err := db.Query(ctx, "SELECT kind, doc FROM "+s.records+" WHERE owner = $1 ORDER BY id", owner)
	if err != nil {
		return core.Catalog{}, err
	}
	defer rows.Close()
	var c core.Catalog
	for rows.Next() {
		var kind string
		var doc []byte
		if err := rows.Scan(&kind, &doc); err != nil {
			return core.Catalog{}, err
		}
		r, err := core.DecodeRecord(core.Kind(kind), doc)
		if err != nil {
			return core.Catalog{}, err
		}
		c.Add(r)
	}
	return c, rows.Err()
}

func (s *Store) writeEntry(ctx context.Context, tx pgx.Tx, e core.Entry, insert bool) error {
	doc, err := json.Marshal(e)
	if err != nil {
		return err
	}
	args := []any{e.ID, e.Owner, e.EmployerID, e.Date.String(), string(e.Kind), e.Minutes, e.Start, e.End, e.ProjectID,
		e.Version, e.Created, e.DeletedAt, doc}
	if insert {
		_, err = tx.Exec(ctx, "INSERT INTO "+s.entries+` (id, owner, employer_id, local_date, kind, minutes, start_utc,
			end_utc, project_id, version, created, deleted_at, doc) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			args...)
		if isUnique(err) {
			return core.ErrConflict
		}
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE "+s.entries+` SET owner = $2, employer_id = $3, local_date = $4, kind = $5, minutes = $6,
		start_utc = $7, end_utc = $8, project_id = $9, version = $10, created = $11, deleted_at = $12, doc = $13
		WHERE id = $1`, args...)
	return err
}

func (s *Store) writeRevision(ctx context.Context, tx pgx.Tx, r core.Revision) error {
	doc, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO "+s.revisions+` (id, owner, record_type, record_id, version, op, at, doc)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, r.ID, r.Owner, string(r.Type), r.RecordID, r.Version, string(r.Op), r.At, doc)
	return err
}

func (s *Store) lockedEntry(ctx context.Context, tx pgx.Tx, owner string, entries ...core.Entry) error {
	c, err := s.catalog(ctx, tx, owner)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if c.Locked(e.EmployerID, e.Date) {
			return core.ErrLocked
		}
	}
	return nil
}

// CreateEntry stores a new entry.
func (s *Store) CreateEntry(ctx context.Context, e core.Entry, c core.Change) (core.Entry, error) {
	e, rev, err := core.PrepareNewEntry(e, c)
	if err != nil {
		return core.Entry{}, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := s.lockedEntry(ctx, tx, e.Owner, e); err != nil {
			return err
		}
		if err := s.writeEntry(ctx, tx, e, true); err != nil {
			return err
		}
		return s.writeRevision(ctx, tx, rev)
	})
	if err != nil {
		return core.Entry{}, err
	}
	return e, nil
}

func (s *Store) entryForUpdate(ctx context.Context, tx pgx.Tx, owner, id string) (core.Entry, error) {
	var doc []byte
	err := tx.QueryRow(ctx, "SELECT doc FROM "+s.entries+" WHERE id = $1 AND owner = $2 FOR UPDATE", id, owner).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.Entry{}, core.ErrNotFound
	}
	if err != nil {
		return core.Entry{}, err
	}
	var e core.Entry
	return e, json.Unmarshal(doc, &e)
}

// UpdateEntry replaces an entry of the stored version.
func (s *Store) UpdateEntry(ctx context.Context, e core.Entry, c core.Change) (core.Entry, error) {
	var next core.Entry
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		old, err := s.entryForUpdate(ctx, tx, e.Owner, e.ID)
		if err != nil {
			return err
		}
		n, rev, err := core.PrepareEntryUpdate(old, e, c)
		if err != nil {
			return err
		}
		if err := s.lockedEntry(ctx, tx, e.Owner, old, n); err != nil {
			return err
		}
		if err := s.writeEntry(ctx, tx, n, false); err != nil {
			return err
		}
		next = n
		return s.writeRevision(ctx, tx, rev)
	})
	if err != nil {
		return core.Entry{}, err
	}
	return next, nil
}

// DeleteEntry marks an entry as deleted.
func (s *Store) DeleteEntry(ctx context.Context, owner, id string, version int, c core.Change) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		old, err := s.entryForUpdate(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		n, rev, err := core.PrepareEntryDeletion(old, owner, version, c)
		if err != nil {
			return err
		}
		if err := s.lockedEntry(ctx, tx, owner, old); err != nil {
			return err
		}
		if err := s.writeEntry(ctx, tx, n, false); err != nil {
			return err
		}
		return s.writeRevision(ctx, tx, rev)
	})
}

// Entry returns one entry.
func (s *Store) Entry(ctx context.Context, owner, id string) (core.Entry, error) {
	var doc []byte
	err := s.db.QueryRow(ctx, "SELECT doc FROM "+s.entries+" WHERE id = $1 AND owner = $2", id, owner).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.Entry{}, core.ErrNotFound
	}
	if err != nil {
		return core.Entry{}, err
	}
	var e core.Entry
	return e, json.Unmarshal(doc, &e)
}

func scanDocs[T any](rows pgx.Rows, err error) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var doc []byte
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal(doc, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Entries returns the matching entries.
func (s *Store) Entries(ctx context.Context, f core.EntryFilter) ([]core.Entry, error) {
	return scanDocs[core.Entry](s.db.Query(ctx, "SELECT doc FROM "+s.entries+` WHERE owner = $1
		AND ($2 = '' OR employer_id = $2)
		AND ($3::date IS NULL OR local_date >= $3::date) AND ($4::date IS NULL OR local_date <= $4::date)
		AND ($5 OR deleted_at IS NULL)
		ORDER BY local_date, start_utc NULLS FIRST, created, id`,
		f.Owner, f.EmployerID, nullDate(f.From), nullDate(f.To), f.IncludeDeleted))
}

func (s *Store) writeAbsence(ctx context.Context, tx pgx.Tx, a core.Absence, insert bool) error {
	doc, err := json.Marshal(a)
	if err != nil {
		return err
	}
	args := []any{a.ID, a.Owner, a.EmployerID, a.Type, a.From.String(), a.To.String(), a.Version, a.DeletedAt, doc}
	if insert {
		_, err = tx.Exec(ctx, "INSERT INTO "+s.absences+` (id, owner, employer_id, type, date_from, date_to, version,
			deleted_at, doc) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, args...)
		if isUnique(err) {
			return core.ErrConflict
		}
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE "+s.absences+` SET owner = $2, employer_id = $3, type = $4, date_from = $5,
		date_to = $6, version = $7, deleted_at = $8, doc = $9 WHERE id = $1`, args...)
	return err
}

func (s *Store) absenceForUpdate(ctx context.Context, tx pgx.Tx, owner, id string) (core.Absence, error) {
	var doc []byte
	err := tx.QueryRow(ctx, "SELECT doc FROM "+s.absences+" WHERE id = $1 AND owner = $2 FOR UPDATE", id, owner).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.Absence{}, core.ErrNotFound
	}
	if err != nil {
		return core.Absence{}, err
	}
	var a core.Absence
	return a, json.Unmarshal(doc, &a)
}

// CreateAbsence stores a new absence.
func (s *Store) CreateAbsence(ctx context.Context, a core.Absence, c core.Change) (core.Absence, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		cat, err := s.catalog(ctx, tx, a.Owner)
		if err != nil {
			return err
		}
		n, rev, err := core.PrepareNewAbsence(a, cat.AbsenceTypesFor(a.EmployerID), c)
		if err != nil {
			return err
		}
		if cat.AbsenceLocked(n) {
			return core.ErrLocked
		}
		if err := s.writeAbsence(ctx, tx, n, true); err != nil {
			return err
		}
		a = n
		return s.writeRevision(ctx, tx, rev)
	})
	if err != nil {
		return core.Absence{}, err
	}
	return a, nil
}

// UpdateAbsence replaces an absence of the stored version.
func (s *Store) UpdateAbsence(ctx context.Context, a core.Absence, c core.Change) (core.Absence, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		old, err := s.absenceForUpdate(ctx, tx, a.Owner, a.ID)
		if err != nil {
			return err
		}
		cat, err := s.catalog(ctx, tx, a.Owner)
		if err != nil {
			return err
		}
		n, rev, err := core.PrepareAbsenceUpdate(old, a, cat.AbsenceTypesFor(a.EmployerID), c)
		if err != nil {
			return err
		}
		if cat.AbsenceLocked(old) || cat.AbsenceLocked(n) {
			return core.ErrLocked
		}
		if err := s.writeAbsence(ctx, tx, n, false); err != nil {
			return err
		}
		a = n
		return s.writeRevision(ctx, tx, rev)
	})
	if err != nil {
		return core.Absence{}, err
	}
	return a, nil
}

// DeleteAbsence marks an absence as deleted.
func (s *Store) DeleteAbsence(ctx context.Context, owner, id string, version int, c core.Change) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		old, err := s.absenceForUpdate(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		cat, err := s.catalog(ctx, tx, owner)
		if err != nil {
			return err
		}
		n, rev, err := core.PrepareAbsenceDeletion(old, owner, version, c)
		if err != nil {
			return err
		}
		if cat.AbsenceLocked(old) {
			return core.ErrLocked
		}
		if err := s.writeAbsence(ctx, tx, n, false); err != nil {
			return err
		}
		return s.writeRevision(ctx, tx, rev)
	})
}

// Absences returns the matching absences.
func (s *Store) Absences(ctx context.Context, f core.AbsenceFilter) ([]core.Absence, error) {
	return scanDocs[core.Absence](s.db.Query(ctx, "SELECT doc FROM "+s.absences+` WHERE owner = $1
		AND ($2 = '' OR employer_id = '' OR employer_id = $2)
		AND ($3::date IS NULL OR date_to >= $3::date) AND ($4::date IS NULL OR date_from <= $4::date)
		AND ($5 OR deleted_at IS NULL)
		ORDER BY date_from, id`,
		f.Owner, f.EmployerID, nullDate(f.From), nullDate(f.To), f.IncludeDeleted))
}

// Revisions returns the history, oldest first.
func (s *Store) Revisions(ctx context.Context, owner, recordID string) ([]core.Revision, error) {
	return scanDocs[core.Revision](s.db.Query(ctx, "SELECT doc FROM "+s.revisions+
		" WHERE owner = $1 AND ($2 = '' OR record_id = $2) ORDER BY seq", owner, recordID))
}

// Purge removes old time records of the class.
func (s *Store) Purge(ctx context.Context, owner string, class core.Class, cutoff core.Date) (int, error) {
	held := `EXISTS (SELECT 1 FROM ` + s.records + ` h WHERE h.kind = 'employer' AND h.owner = x.owner
		AND (h.doc->>'hold')::boolean AND %s)`
	var removed int
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		switch class {
		case core.ClassTimeRecords:
			err = tx.QueryRow(ctx, `WITH gone_e AS (
				DELETE FROM `+s.entries+` x WHERE ($1 = '' OR x.owner = $1) AND x.local_date < $2::date
					AND NOT `+fmt.Sprintf(held, "h.id = x.employer_id")+` RETURNING x.id),
			gone_a AS (
				DELETE FROM `+s.absences+` x WHERE ($1 = '' OR x.owner = $1) AND x.date_to < $2::date
					AND NOT `+fmt.Sprintf(held, "(x.employer_id = '' OR h.id = x.employer_id)")+` RETURNING x.id),
			gone_r AS (
				DELETE FROM `+s.revisions+` WHERE record_id IN (SELECT id FROM gone_e UNION ALL SELECT id FROM gone_a))
			SELECT (SELECT count(*) FROM gone_e) + (SELECT count(*) FROM gone_a)`, owner, cutoff.String()).Scan(&removed)
		case core.ClassLeaveNotices:
			err = tx.QueryRow(ctx, `WITH gone AS (
				DELETE FROM `+s.records+` x WHERE x.kind = 'leave_notice' AND ($1 = '' OR x.owner = $1)
					AND (x.doc->>'year')::int < $2 AND NOT `+fmt.Sprintf(held, "h.id = x.employer_id")+` RETURNING x.id)
			SELECT count(*) FROM gone`, owner, cutoff.Year).Scan(&removed)
		default:
			return core.Retention{Class: class, YearsAfterYearEnd: core.MinimumYears}.Validate()
		}
		if err != nil {
			return err
		}
		log := core.PurgeLog{ID: core.NewID(), Owner: owner, Class: class, Cutoff: cutoff, Removed: removed,
			At: time.Now().UTC()}
		doc, err := json.Marshal(log)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO "+s.records+" (kind, id, owner, doc) VALUES ($1, $2, $3, $4)",
			core.RecPurgeLog, log.ID, owner, doc)
		return err
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// Erase removes everything private of the owner; the records of managed employers stay.
func (s *Store) Erase(ctx context.Context, owner string) error {
	if owner == "" {
		return core.ErrInvalid
	}
	managed := "(SELECT id FROM " + s.records + " WHERE kind = 'employer' AND owner = $1 AND doc->>'holder' = 'managed')"
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		for _, q := range []string{
			"DELETE FROM " + s.revisions + " WHERE owner = $1 AND record_id IN (SELECT id FROM " + s.entries +
				" WHERE owner = $1 AND employer_id NOT IN " + managed + " UNION ALL SELECT id FROM " + s.absences +
				" WHERE owner = $1 AND employer_id NOT IN " + managed + ")",
			"DELETE FROM " + s.entries + " WHERE owner = $1 AND employer_id NOT IN " + managed,
			"DELETE FROM " + s.absences + " WHERE owner = $1 AND employer_id NOT IN " + managed,
			"DELETE FROM " + s.records + " WHERE owner = $1 AND employer_id NOT IN " + managed,
		} {
			if _, err := tx.Exec(ctx, q, owner); err != nil {
				return err
			}
		}
		return nil
	})
}

// Managed returns what the organization may see.
func (s *Store) Managed(ctx context.Context, organization string, from, to core.Date) (core.Data, error) {
	if organization == "" {
		return core.Data{}, core.ErrInvalid
	}
	// The managed employers of the organization; every row below must match one of them by employer AND owner.
	emps := "(SELECT id, owner FROM " + s.records +
		" WHERE kind = 'employer' AND doc->>'holder' = 'managed' AND doc->>'organization' = $1)"
	var d core.Data
	rows, err := s.db.Query(ctx, "SELECT r.kind, r.doc FROM "+s.records+" r JOIN "+emps+
		" e ON r.employer_id = e.id AND r.owner = e.owner ORDER BY r.id", organization)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var doc []byte
		if err := rows.Scan(&kind, &doc); err != nil {
			return d, err
		}
		r, err := core.DecodeRecord(core.Kind(kind), doc)
		if err != nil {
			return d, err
		}
		d.Catalog.Add(r)
	}
	if err := rows.Err(); err != nil {
		return d, err
	}
	rows.Close()
	d.Entries, err = scanDocs[core.Entry](s.db.Query(ctx, "SELECT x.doc FROM "+s.entries+" x JOIN "+emps+
		` e ON x.employer_id = e.id AND x.owner = e.owner WHERE x.deleted_at IS NULL
		AND ($2::date IS NULL OR x.local_date >= $2::date) AND ($3::date IS NULL OR x.local_date <= $3::date)
		ORDER BY x.local_date, x.start_utc NULLS FIRST, x.created, x.id`, organization, nullDate(from), nullDate(to)))
	if err != nil {
		return d, err
	}
	d.Absences, err = scanDocs[core.Absence](s.db.Query(ctx, "SELECT x.doc FROM "+s.absences+" x JOIN "+emps+
		` e ON x.employer_id = e.id AND x.owner = e.owner WHERE x.deleted_at IS NULL
		AND ($2::date IS NULL OR x.date_to >= $2::date) AND ($3::date IS NULL OR x.date_from <= $3::date)
		ORDER BY x.date_from, x.id`, organization, nullDate(from), nullDate(to)))
	return d, err
}

// Organizations and memberships live in the records table with an empty owner, so they belong to no person's
// catalog and no erasure of a person reaches them.

// PutOrganization inserts or replaces an organization.
func (s *Store) PutOrganization(ctx context.Context, o core.Organization) error {
	if err := o.Validate(); err != nil {
		return err
	}
	doc, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, "INSERT INTO "+s.records+` (kind, id, owner, doc) VALUES ('organization', $1, '', $2)
		ON CONFLICT (tenant, kind, id) DO UPDATE SET doc = EXCLUDED.doc`, o.ID, doc)
	return err
}

// Organization returns one organization.
func (s *Store) Organization(ctx context.Context, id string) (core.Organization, error) {
	var doc []byte
	err := s.db.QueryRow(ctx, "SELECT doc FROM "+s.records+" WHERE kind = 'organization' AND id = $1", id).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.Organization{}, core.ErrNotFound
	}
	if err != nil {
		return core.Organization{}, err
	}
	var o core.Organization
	return o, json.Unmarshal(doc, &o)
}

// PutMembership inserts or replaces a membership.
func (s *Store) PutMembership(ctx context.Context, m core.Membership) error {
	if err := m.Validate(); err != nil {
		return err
	}
	doc, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+s.records+" WHERE kind = 'organization' AND id = $1",
			m.Organization).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return core.ErrNotFound
		}
		tag, err := tx.Exec(ctx, "INSERT INTO "+s.records+` AS r (kind, id, owner, doc) VALUES ('membership', $1, '', $2)
			ON CONFLICT (tenant, kind, id) DO UPDATE SET doc = EXCLUDED.doc
			WHERE r.doc->>'organization' = EXCLUDED.doc->>'organization' AND r.doc->>'person' = EXCLUDED.doc->>'person'`,
			m.ID, doc)
		if err == nil && tag.RowsAffected() == 0 {
			return core.ErrConflict
		}
		return err
	})
}

// Memberships returns the matching memberships ordered by start and ID.
func (s *Store) Memberships(ctx context.Context, organization, person string) ([]core.Membership, error) {
	return scanDocs[core.Membership](s.db.Query(ctx, "SELECT doc FROM "+s.records+` WHERE kind = 'membership'
		AND ($1 = '' OR doc->>'organization' = $1) AND ($2 = '' OR doc->>'person' = $2)
		ORDER BY (doc->>'from')::date, id`, organization, person))
}
