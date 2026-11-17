// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"math"
	"time"
)

// Normalize makes Minutes, Date and Precision follow Start and End when both are set (start and end win), stores them
// in UTC and fills the defaults: precision day, source manual, time zone of the entry.
func (e *Entry) Normalize() error {
	if e.Source == "" {
		e.Source = SourceManual
	}
	if e.Start == nil || e.End == nil {
		if e.Precision == "" {
			e.Precision = PrecisionDay
		}
		return nil
	}
	name := e.TimeZone
	if name == "" {
		name = DefaultTimeZone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return invalid("time zone %q", e.TimeZone)
	}
	start, end := e.Start.UTC(), e.End.UTC()
	e.Start, e.End = &start, &end
	e.TimeZone = name
	e.Minutes = int(math.Round(end.Sub(start).Minutes()))
	e.Date = DateIn(start, loc)
	if e.Precision == "" {
		e.Precision = PrecisionSpan
	}
	return nil
}

func revision(owner string, t RecordType, id string, version int, op Operation, c Change, before, after any) (Revision, error) {
	r := Revision{ID: NewID(), Owner: owner, Type: t, RecordID: id, Version: version, Op: op, At: c.When(), By: c.By,
		Reason: c.Reason}
	if before != nil {
		b, err := json.Marshal(before)
		if err != nil {
			return Revision{}, err
		}
		r.Before = b
	}
	a, err := json.Marshal(after)
	if err != nil {
		return Revision{}, err
	}
	r.After = a
	return r, nil
}

// PrepareNewEntry is the shared part of Store.CreateEntry: it normalises and validates the entry, gives it an ID,
// version 1 and its times, and returns the revision to store with it.
func PrepareNewEntry(e Entry, c Change) (Entry, Revision, error) {
	if e.ID == "" {
		e.ID = NewID()
	}
	if err := e.Normalize(); err != nil {
		return Entry{}, Revision{}, err
	}
	now := c.When()
	e.Version, e.Created, e.Changed, e.DeletedAt = 1, now, now, nil
	if err := e.Validate(); err != nil {
		return Entry{}, Revision{}, err
	}
	r, err := revision(e.Owner, RecordEntry, e.ID, 1, OpCreate, c, nil, e)
	return e, r, err
}

// PrepareEntryUpdate is the shared part of Store.UpdateEntry: old is the stored entry. It fails with ErrConflict when
// e.Version is not the stored version, and with ErrNotFound for a deleted or foreign entry.
func PrepareEntryUpdate(old, e Entry, c Change) (Entry, Revision, error) {
	if old.Owner != e.Owner || old.DeletedAt != nil {
		return Entry{}, Revision{}, ErrNotFound
	}
	if old.Version != e.Version {
		return Entry{}, Revision{}, ErrConflict
	}
	if err := e.Normalize(); err != nil {
		return Entry{}, Revision{}, err
	}
	e.Version, e.Created, e.Changed, e.DeletedAt = old.Version+1, old.Created, c.When(), nil
	if err := e.Validate(); err != nil {
		return Entry{}, Revision{}, err
	}
	r, err := revision(e.Owner, RecordEntry, e.ID, e.Version, OpUpdate, c, old, e)
	return e, r, err
}

// PrepareEntryDeletion is the shared part of Store.DeleteEntry.
func PrepareEntryDeletion(old Entry, owner string, version int, c Change) (Entry, Revision, error) {
	if old.Owner != owner || old.DeletedAt != nil {
		return Entry{}, Revision{}, ErrNotFound
	}
	if old.Version != version {
		return Entry{}, Revision{}, ErrConflict
	}
	e := old
	now := c.When()
	e.Version, e.Changed, e.DeletedAt = old.Version+1, now, &now
	r, err := revision(e.Owner, RecordEntry, e.ID, e.Version, OpDelete, c, old, e)
	return e, r, err
}

// PrepareNewAbsence is the shared part of Store.CreateAbsence. An empty status means approved.
func PrepareNewAbsence(a Absence, types []AbsenceType, c Change) (Absence, Revision, error) {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.Status == "" {
		a.Status = StatusApproved
	}
	now := c.When()
	a.Version, a.Created, a.Changed, a.DeletedAt = 1, now, now, nil
	if err := a.Validate(types); err != nil {
		return Absence{}, Revision{}, err
	}
	r, err := revision(a.Owner, RecordAbsence, a.ID, 1, OpCreate, c, nil, a)
	return a, r, err
}

// PrepareAbsenceUpdate is the shared part of Store.UpdateAbsence.
func PrepareAbsenceUpdate(old, a Absence, types []AbsenceType, c Change) (Absence, Revision, error) {
	if old.Owner != a.Owner || old.DeletedAt != nil {
		return Absence{}, Revision{}, ErrNotFound
	}
	if old.Version != a.Version {
		return Absence{}, Revision{}, ErrConflict
	}
	if a.Status == "" {
		a.Status = StatusApproved
	}
	a.Version, a.Created, a.Changed, a.DeletedAt = old.Version+1, old.Created, c.When(), nil
	if err := a.Validate(types); err != nil {
		return Absence{}, Revision{}, err
	}
	r, err := revision(a.Owner, RecordAbsence, a.ID, a.Version, OpUpdate, c, old, a)
	return a, r, err
}

// PrepareAbsenceDeletion is the shared part of Store.DeleteAbsence.
func PrepareAbsenceDeletion(old Absence, owner string, version int, c Change) (Absence, Revision, error) {
	if old.Owner != owner || old.DeletedAt != nil {
		return Absence{}, Revision{}, ErrNotFound
	}
	if old.Version != version {
		return Absence{}, Revision{}, ErrConflict
	}
	a := old
	now := c.When()
	a.Version, a.Changed, a.DeletedAt = old.Version+1, now, &now
	r, err := revision(a.Owner, RecordAbsence, a.ID, a.Version, OpDelete, c, old, a)
	return a, r, err
}

// AbsenceLocked reports whether any day of the absence lies in a closed period of its employer (or of any private
// employer, for an absence that covers all of them), or in a period of its employer handed in for approval.
func (c Catalog) AbsenceLocked(a Absence) bool {
	for _, p := range c.PeriodCloses {
		emp, _ := c.Employer(p.EmployerID)
		if appliesTo(emp, a.EmployerID) && !a.To.Before(p.From) && !a.From.After(p.To) {
			return true
		}
	}
	for _, s := range c.Submissions {
		if s.Kind == SubmitPeriod && s.EmployerID == a.EmployerID && a.EmployerID != "" && !a.To.Before(s.From) &&
			!a.From.After(s.To) && c.binding(s) {
			return true
		}
	}
	return false
}
