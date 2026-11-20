// SPDX-License-Identifier: MIT

// Package memstore keeps the data of EightPerDay in memory. It is meant for tests and for trying the library out; it
// follows the same rules as core/pgstore (history, versions, closed periods), and core/storetest proves that both do.
package memstore

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

type key struct {
	kind core.Kind
	id   string
}

// Store is an in-memory core.Store. The zero value is not usable; call New.
type Store struct {
	mu        sync.Mutex
	records   map[key][]byte // JSON, so callers cannot change stored values through shared slices
	meta      map[key]core.Meta
	entries   map[string]core.Entry
	absences  map[string]core.Absence
	revisions []core.Revision
	orgs      map[string]core.Organization
	members   map[string]core.Membership
	accesses  []core.Access
	timers    map[string]core.Timer
}

var (
	_ core.Store       = (*Store)(nil)
	_ core.AccessLog   = (*Store)(nil)
	_ core.Stopwatches = (*Store)(nil)
)

// New returns an empty store.
func New() *Store {
	return &Store{records: map[key][]byte{}, meta: map[key]core.Meta{}, entries: map[string]core.Entry{},
		absences: map[string]core.Absence{}, orgs: map[string]core.Organization{}, members: map[string]core.Membership{},
		timers: map[string]core.Timer{}}
}

// Put inserts or replaces a catalog record.
func (s *Store) Put(_ context.Context, r core.Record) error {
	if err := r.Validate(); err != nil {
		return err
	}
	m := r.Meta()
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{m.Kind, m.ID}
	if old, ok := s.meta[k]; ok {
		if old.Owner != m.Owner {
			return core.ErrNotFound
		}
		stored, err := core.DecodeRecord(k.kind, s.records[k])
		if err != nil {
			return err
		}
		if !core.Replaceable(stored) {
			return core.ErrLocked
		}
		if err := s.dayLocked(m.Owner, stored); err != nil {
			return err
		}
	}
	if err := s.dayLocked(m.Owner, r); err != nil {
		return err
	}
	s.records[k], s.meta[k] = data, m
	return nil
}

// dayLocked refuses a record of a locked day (core.Catalog.DayRecordLocked).
func (s *Store) dayLocked(owner string, r core.Record) error {
	if m := r.Meta(); m.Kind != core.RecDayInfo && m.Kind != core.RecIncome {
		return nil
	}
	c, err := s.catalog(owner)
	if err != nil {
		return err
	}
	if c.DayRecordLocked(r) {
		return core.ErrLocked
	}
	return nil
}

// Delete removes a catalog record.
func (s *Store) Delete(_ context.Context, kind core.Kind, owner, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{kind, id}
	m, ok := s.meta[k]
	if !ok || m.Owner != owner {
		return core.ErrNotFound
	}
	if core.Fixed(kind) {
		return core.ErrLocked
	}
	stored, err := core.DecodeRecord(kind, s.records[k])
	if err != nil {
		return err
	}
	if err := s.dayLocked(owner, stored); err != nil {
		return err
	}
	delete(s.records, k)
	delete(s.meta, k)
	return nil
}

// Catalog returns every catalog record of the owner, in the order of their IDs.
func (s *Store) Catalog(_ context.Context, owner string) (core.Catalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.catalog(owner)
}

func (s *Store) catalog(owner string) (core.Catalog, error) {
	var keys []key
	for k, m := range s.meta {
		if m.Owner == owner {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b key) int {
		if a.id < b.id {
			return -1
		}
		if a.id > b.id {
			return 1
		}
		return 0
	})
	var c core.Catalog
	for _, k := range keys {
		r, err := core.DecodeRecord(k.kind, s.records[k])
		if err != nil {
			return core.Catalog{}, err
		}
		c.Add(r)
	}
	return c, nil
}

func (s *Store) lockedEntry(owner string, e core.Entry) (bool, error) {
	c, err := s.catalog(owner)
	if err != nil {
		return false, err
	}
	return c.Locked(e.EmployerID, e.Date), nil
}

// CreateEntry stores a new entry.
func (s *Store) CreateEntry(_ context.Context, e core.Entry, c core.Change) (core.Entry, error) {
	e, rev, err := core.PrepareNewEntry(e, c)
	if err != nil {
		return core.Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[e.ID]; ok {
		return core.Entry{}, core.ErrConflict
	}
	if locked, err := s.lockedEntry(e.Owner, e); err != nil || locked {
		return core.Entry{}, lockedErr(err)
	}
	s.entries[e.ID] = e
	s.revisions = append(s.revisions, rev)
	return clone(e), nil
}

func lockedErr(err error) error {
	if err != nil {
		return err
	}
	return core.ErrLocked
}

// UpdateEntry replaces an entry of the stored version.
func (s *Store) UpdateEntry(_ context.Context, e core.Entry, c core.Change) (core.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.entries[e.ID]
	if !ok || old.Owner != e.Owner {
		return core.Entry{}, core.ErrNotFound
	}
	next, rev, err := core.PrepareEntryUpdate(old, e, c)
	if err != nil {
		return core.Entry{}, err
	}
	for _, x := range []core.Entry{old, next} {
		if locked, err := s.lockedEntry(e.Owner, x); err != nil || locked {
			return core.Entry{}, lockedErr(err)
		}
	}
	s.entries[e.ID] = next
	s.revisions = append(s.revisions, rev)
	return clone(next), nil
}

// DeleteEntry marks an entry as deleted.
func (s *Store) DeleteEntry(_ context.Context, owner, id string, version int, c core.Change) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.entries[id]
	if !ok || old.Owner != owner {
		return core.ErrNotFound
	}
	next, rev, err := core.PrepareEntryDeletion(old, owner, version, c)
	if err != nil {
		return err
	}
	if locked, err := s.lockedEntry(owner, old); err != nil || locked {
		return lockedErr(err)
	}
	s.entries[id] = next
	s.revisions = append(s.revisions, rev)
	return nil
}

// Entry returns one entry.
func (s *Store) Entry(_ context.Context, owner, id string) (core.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok || e.Owner != owner {
		return core.Entry{}, core.ErrNotFound
	}
	return clone(e), nil
}

// Entries returns the matching entries ordered by date, start and creation.
func (s *Store) Entries(_ context.Context, f core.EntryFilter) ([]core.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Entry
	for _, e := range s.entries {
		if e.Owner != f.Owner || (f.EmployerID != "" && e.EmployerID != f.EmployerID) ||
			(e.DeletedAt != nil && !f.IncludeDeleted) ||
			(!f.From.IsZero() && e.Date.Before(f.From)) || (!f.To.IsZero() && e.Date.After(f.To)) {
			continue
		}
		out = append(out, clone(e))
	}
	slices.SortFunc(out, compareEntries)
	return out, nil
}

func compareEntries(a, b core.Entry) int {
	if c := a.Date.Compare(b.Date); c != 0 {
		return c
	}
	as, bs := time.Time{}, time.Time{}
	if a.Start != nil {
		as = *a.Start
	}
	if b.Start != nil {
		bs = *b.Start
	}
	if c := as.Compare(bs); c != 0 {
		return c
	}
	if c := a.Created.Compare(b.Created); c != 0 {
		return c
	}
	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}
	return 0
}

func clone(e core.Entry) core.Entry {
	e.Tags = slices.Clone(e.Tags)
	if e.Rate != nil {
		r := *e.Rate
		e.Rate = &r
	}
	return e
}

func (s *Store) absenceTypes(owner, employerID string) ([]core.AbsenceType, core.Catalog, error) {
	c, err := s.catalog(owner)
	if err != nil {
		return nil, core.Catalog{}, err
	}
	return c.AbsenceTypesFor(employerID), c, nil
}

// CreateAbsence stores a new absence.
func (s *Store) CreateAbsence(_ context.Context, a core.Absence, c core.Change) (core.Absence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	types, cat, err := s.absenceTypes(a.Owner, a.EmployerID)
	if err != nil {
		return core.Absence{}, err
	}
	a, rev, err := core.PrepareNewAbsence(a, types, c)
	if err != nil {
		return core.Absence{}, err
	}
	if _, ok := s.absences[a.ID]; ok {
		return core.Absence{}, core.ErrConflict
	}
	if cat.AbsenceLocked(a) {
		return core.Absence{}, core.ErrLocked
	}
	s.absences[a.ID] = a
	s.revisions = append(s.revisions, rev)
	return a, nil
}

// UpdateAbsence replaces an absence of the stored version.
func (s *Store) UpdateAbsence(_ context.Context, a core.Absence, c core.Change) (core.Absence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.absences[a.ID]
	if !ok || old.Owner != a.Owner {
		return core.Absence{}, core.ErrNotFound
	}
	types, cat, err := s.absenceTypes(a.Owner, a.EmployerID)
	if err != nil {
		return core.Absence{}, err
	}
	next, rev, err := core.PrepareAbsenceUpdate(old, a, types, c)
	if err != nil {
		return core.Absence{}, err
	}
	if cat.AbsenceLocked(old) || cat.AbsenceLocked(next) || cat.LeaveLocked(old, &next) {
		return core.Absence{}, core.ErrLocked
	}
	s.absences[a.ID] = next
	s.revisions = append(s.revisions, rev)
	return next, nil
}

// DeleteAbsence marks an absence as deleted.
func (s *Store) DeleteAbsence(_ context.Context, owner, id string, version int, c core.Change) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.absences[id]
	if !ok || old.Owner != owner {
		return core.ErrNotFound
	}
	_, cat, err := s.absenceTypes(owner, "")
	if err != nil {
		return err
	}
	next, rev, err := core.PrepareAbsenceDeletion(old, owner, version, c)
	if err != nil {
		return err
	}
	if cat.AbsenceLocked(old) || cat.LeaveLocked(old, nil) {
		return core.ErrLocked
	}
	s.absences[id] = next
	s.revisions = append(s.revisions, rev)
	return nil
}

// Absences returns the matching absences ordered by start.
func (s *Store) Absences(_ context.Context, f core.AbsenceFilter) ([]core.Absence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Absence
	for _, a := range s.absences {
		if a.Owner != f.Owner || (f.EmployerID != "" && a.EmployerID != "" && a.EmployerID != f.EmployerID) ||
			(a.DeletedAt != nil && !f.IncludeDeleted) ||
			(!f.From.IsZero() && a.To.Before(f.From)) || (!f.To.IsZero() && a.From.After(f.To)) {
			continue
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b core.Absence) int {
		if c := a.From.Compare(b.From); c != 0 {
			return c
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out, nil
}

// Revisions returns the history, oldest first.
func (s *Store) Revisions(_ context.Context, owner, recordID string) ([]core.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Revision
	for _, r := range s.revisions {
		if r.Owner == owner && (recordID == "" || r.RecordID == recordID) {
			out = append(out, r)
		}
	}
	return out, nil
}

// held returns the employers on legal hold, and the owners that have one.
func (s *Store) held(owner string) (employers, owners map[string]bool) {
	employers, owners = map[string]bool{}, map[string]bool{}
	for k, m := range s.meta {
		if k.kind == core.RecEmployer && (owner == "" || m.Owner == owner) {
			if r, err := core.DecodeRecord(k.kind, s.records[k]); err == nil && r.(core.Employer).Hold {
				employers[m.ID], owners[m.Owner] = true, true
			}
		}
	}
	return employers, owners
}

// Purge removes old time records of the class.
func (s *Store) Purge(_ context.Context, owner string, class core.Class, cutoff core.Date) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held, heldOwners := s.held(owner)
	removed := 0
	gone := map[string]bool{}
	switch class {
	case core.ClassTimeRecords:
		for id, e := range s.entries {
			if (owner == "" || e.Owner == owner) && e.Date.Before(cutoff) && !held[e.EmployerID] {
				delete(s.entries, id)
				gone[id] = true
				removed++
			}
		}
		for id, a := range s.absences {
			// An absence for every employer stays while any employer of its owner is on hold.
			if (owner == "" || a.Owner == owner) && a.To.Before(cutoff) && !held[a.EmployerID] &&
				(a.EmployerID != "" || !heldOwners[a.Owner]) {
				delete(s.absences, id)
				gone[id] = true
				removed++
			}
		}
		s.revisions = slices.DeleteFunc(s.revisions, func(r core.Revision) bool { return gone[r.RecordID] })
	case core.ClassLeaveNotices:
		for k, m := range s.meta {
			if k.kind != core.RecLeaveNotice || (owner != "" && m.Owner != owner) || held[m.EmployerID] {
				continue
			}
			r, err := core.DecodeRecord(k.kind, s.records[k])
			if err != nil {
				return 0, err
			}
			if r.(core.LeaveNotice).Year < cutoff.Year {
				delete(s.records, k)
				delete(s.meta, k)
				removed++
			}
		}
	case core.ClassAccessLog:
		before := len(s.accesses)
		s.accesses = slices.DeleteFunc(s.accesses, func(a core.Access) bool {
			// As in PostgreSQL: nothing younger than the minimum retention goes, whatever the cutoff.
			return (owner == "" || a.Subject == owner) && core.DateOf(a.At).Before(cutoff) &&
				a.At.Before(time.Now().AddDate(-core.MinimumYears, 0, 0))
		})
		removed = before - len(s.accesses)
	default:
		return 0, core.Retention{Class: class, YearsAfterYearEnd: core.MinimumYears}.Validate()
	}
	log := core.PurgeLog{ID: core.NewID(), Owner: owner, Class: class, Cutoff: cutoff, Removed: removed,
		At: time.Now().UTC()}
	data, err := json.Marshal(log)
	if err != nil {
		return 0, err
	}
	k := key{core.RecPurgeLog, log.ID}
	s.records[k], s.meta[k] = data, log.Meta()
	return removed, nil
}

// managedEmployers returns the managed employers that match: of one owner, or of one organization.
func (s *Store) managedEmployers(owner, organization string) map[string]string {
	out := map[string]string{} // employer ID to owner
	for k, m := range s.meta {
		if k.kind != core.RecEmployer || (owner != "" && m.Owner != owner) {
			continue
		}
		r, err := core.DecodeRecord(k.kind, s.records[k])
		if err != nil {
			continue
		}
		e := r.(core.Employer)
		if e.Managed() && (organization == "" || e.Organization == organization) {
			out[e.ID] = e.Owner
		}
	}
	return out
}

// Erase removes everything private of the owner; the records of managed employers stay.
func (s *Store) Erase(_ context.Context, owner string) error {
	if owner == "" {
		return core.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	managed := s.managedEmployers(owner, "")
	keep := func(employerID string) bool { _, ok := managed[employerID]; return ok }
	for k, m := range s.meta {
		if m.Owner == owner && !keep(m.EmployerID) {
			delete(s.records, k)
			delete(s.meta, k)
		}
	}
	gone := map[string]bool{}
	for id, e := range s.entries {
		if e.Owner == owner && !keep(e.EmployerID) {
			delete(s.entries, id)
			gone[id] = true
		}
	}
	for id, a := range s.absences {
		if a.Owner == owner && !keep(a.EmployerID) {
			delete(s.absences, id)
			gone[id] = true
		}
	}
	s.revisions = slices.DeleteFunc(s.revisions, func(r core.Revision) bool { return r.Owner == owner && gone[r.RecordID] })
	delete(s.timers, owner)
	return nil
}

// Managed returns what the organization may see.
func (s *Store) Managed(_ context.Context, organization string, from, to core.Date) (core.Data, error) {
	if organization == "" {
		return core.Data{}, core.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	emps := s.managedEmployers("", organization)
	ours := func(owner, employerID string) bool { o, ok := emps[employerID]; return ok && o == owner }
	var keys []key
	for k, m := range s.meta {
		if ours(m.Owner, m.EmployerID) {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b key) int { return strings.Compare(a.id, b.id) })
	var d core.Data
	for _, k := range keys {
		r, err := core.DecodeRecord(k.kind, s.records[k])
		if err != nil {
			return core.Data{}, err
		}
		d.Catalog.Add(r)
	}
	for _, e := range s.entries {
		if ours(e.Owner, e.EmployerID) && e.DeletedAt == nil && e.Date.Within(from, to) {
			d.Entries = append(d.Entries, clone(e))
		}
	}
	slices.SortFunc(d.Entries, compareEntries)
	for _, a := range s.absences {
		if ours(a.Owner, a.EmployerID) && a.DeletedAt == nil && !a.To.Before(from) && (to.IsZero() || !a.From.After(to)) {
			d.Absences = append(d.Absences, a)
		}
	}
	slices.SortFunc(d.Absences, func(a, b core.Absence) int {
		if c := a.From.Compare(b.From); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return d, nil
}

// PutOrganization inserts or replaces an organization.
func (s *Store) PutOrganization(_ context.Context, o core.Organization) error {
	if err := o.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	var c core.Organization
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orgs[o.ID] = c
	return nil
}

// Organization returns one organization.
func (s *Store) Organization(_ context.Context, id string) (core.Organization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orgs[id]
	if !ok {
		return core.Organization{}, core.ErrNotFound
	}
	data, err := json.Marshal(o)
	if err != nil {
		return core.Organization{}, err
	}
	var c core.Organization
	return c, json.Unmarshal(data, &c)
}

// PutMembership inserts or replaces a membership.
func (s *Store) PutMembership(_ context.Context, m core.Membership) error {
	if err := m.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[m.Organization]; !ok {
		return core.ErrNotFound
	}
	if old, ok := s.members[m.ID]; ok && (old.Organization != m.Organization || old.Person != m.Person) {
		return core.ErrConflict
	}
	s.members[m.ID] = m
	return nil
}

// Memberships returns the matching memberships ordered by start and ID.
func (s *Store) Memberships(_ context.Context, organization, person string) ([]core.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Membership
	for _, m := range s.members {
		if (organization == "" || m.Organization == organization) && (person == "" || m.Person == person) {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b core.Membership) int {
		if c := a.From.Compare(b.From); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// LogAccess adds an entry to the access log, linked to the subject's last one.
func (s *Store) LogAccess(_ context.Context, a core.Access) (core.Access, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := ""
	for _, x := range s.accesses {
		if x.Subject == a.Subject {
			prev = x.Hash
		}
	}
	a, err := core.PrepareAccess(a, prev)
	if err != nil {
		return core.Access{}, err
	}
	s.accesses = append(s.accesses, a)
	return a, nil
}

// Accesses returns the access log of the subject, oldest first.
func (s *Store) Accesses(_ context.Context, subject string) ([]core.Access, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Access
	for _, a := range s.accesses {
		if a.Subject == subject {
			out = append(out, a)
		}
	}
	return out, nil
}

// RunningTimer returns the stopwatch of the person, or nil.
func (s *Store) RunningTimer(_ context.Context, person string) (*core.Timer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.timers[person]
	if !ok {
		return nil, nil
	}
	return &t, nil
}

// StartTimer starts the stopwatch of the person; only one runs at a time.
func (s *Store) StartTimer(_ context.Context, person string, t core.Timer) error {
	t.Owner = person
	if err := t.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.timers[person]; ok {
		return core.ErrTimerRunning
	}
	t.Started = t.Started.UTC()
	s.timers[person] = t
	return nil
}

// StopTimer removes the stopwatch of the person and returns it, or nil when none ran.
func (s *Store) StopTimer(_ context.Context, person string) (*core.Timer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.timers[person]
	if !ok {
		return nil, nil
	}
	delete(s.timers, person)
	return &t, nil
}
