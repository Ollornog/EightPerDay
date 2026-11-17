// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

// Kind names a type of setting record in the catalog.
type Kind string

const (
	RecEmployer     Kind = "employer"
	RecModeChange   Kind = "mode_change"
	RecSchedule     Kind = "schedule"
	RecPayRule      Kind = "pay_rule"
	RecClient       Kind = "client"
	RecProject      Kind = "project"
	RecActivity     Kind = "activity"
	RecTag          Kind = "tag"
	RecAbsenceType  Kind = "absence_type"
	RecConfirmation Kind = "confirmation"
	RecAdjustment   Kind = "adjustment"
	RecPeriodClose  Kind = "period_close"
	RecHoliday      Kind = "holiday"
	RecLeaveNotice  Kind = "leave_notice"
	RecPurgeLog     Kind = "purge_log"
)

// Kinds lists every catalog kind.
var Kinds = []Kind{RecEmployer, RecModeChange, RecSchedule, RecPayRule, RecClient, RecProject, RecActivity,
	RecTag, RecAbsenceType, RecConfirmation, RecAdjustment, RecPeriodClose, RecHoliday, RecLeaveNotice, RecPurgeLog}

// Meta is what a store needs to know about a catalog record to file it.
type Meta struct {
	Kind       Kind
	ID         string
	Owner      string
	EmployerID string
}

// Record is a setting record of the catalog: everything except entries and absences, which carry a history.
type Record interface {
	Meta() Meta
	Validate() error
}

func (e Employer) Meta() Meta { return Meta{RecEmployer, e.ID, e.Owner, e.ID} }
func (m ModeChange) Meta() Meta {
	return Meta{RecModeChange, m.ID, m.Owner, m.EmployerID}
}
func (s Schedule) Meta() Meta    { return Meta{RecSchedule, s.ID, s.Owner, s.EmployerID} }
func (p PayRule) Meta() Meta     { return Meta{RecPayRule, p.ID, p.Owner, p.EmployerID} }
func (c Client) Meta() Meta      { return Meta{RecClient, c.ID, c.Owner, c.EmployerID} }
func (p Project) Meta() Meta     { return Meta{RecProject, p.ID, p.Owner, p.EmployerID} }
func (a Activity) Meta() Meta    { return Meta{RecActivity, a.ID, a.Owner, ""} }
func (t Tag) Meta() Meta         { return Meta{RecTag, t.ID, t.Owner, ""} }
func (t AbsenceType) Meta() Meta { return Meta{RecAbsenceType, t.ID, t.Owner, t.EmployerID} }
func (c Confirmation) Meta() Meta {
	return Meta{RecConfirmation, c.ID, c.Owner, c.EmployerID}
}
func (a Adjustment) Meta() Meta  { return Meta{RecAdjustment, a.ID, a.Owner, a.EmployerID} }
func (p PeriodClose) Meta() Meta { return Meta{RecPeriodClose, p.ID, p.Owner, p.EmployerID} }
func (h Holiday) Meta() Meta     { return Meta{RecHoliday, h.ID, h.Owner, h.EmployerID} }
func (n LeaveNotice) Meta() Meta { return Meta{RecLeaveNotice, n.ID, n.Owner, n.EmployerID} }
func (p PurgeLog) Meta() Meta    { return Meta{RecPurgeLog, p.ID, p.Owner, ""} }

// Catalog holds every setting record of one owner. It is small, so it is loaded as a whole.
type Catalog struct {
	Employers     []Employer     `json:"employers"`
	ModeChanges   []ModeChange   `json:"mode_changes"`
	Schedules     []Schedule     `json:"schedules"`
	PayRules      []PayRule      `json:"pay_rules"`
	Clients       []Client       `json:"clients"`
	Projects      []Project      `json:"projects"`
	Activities    []Activity     `json:"activities"`
	Tags          []Tag          `json:"tags"`
	AbsenceTypes  []AbsenceType  `json:"absence_types"`
	Confirmations []Confirmation `json:"confirmations"`
	Adjustments   []Adjustment   `json:"adjustments"`
	PeriodCloses  []PeriodClose  `json:"period_closes"`
	Holidays      []Holiday      `json:"holidays"`
	LeaveNotices  []LeaveNotice  `json:"leave_notices"`
	PurgeLogs     []PurgeLog     `json:"purge_logs"`
}

// Add files a record into the catalog. Stores use it to build a catalog from stored records.
func (c *Catalog) Add(r Record) {
	switch v := r.(type) {
	case Employer:
		c.Employers = append(c.Employers, v)
	case ModeChange:
		c.ModeChanges = append(c.ModeChanges, v)
	case Schedule:
		c.Schedules = append(c.Schedules, v)
	case PayRule:
		c.PayRules = append(c.PayRules, v)
	case Client:
		c.Clients = append(c.Clients, v)
	case Project:
		c.Projects = append(c.Projects, v)
	case Activity:
		c.Activities = append(c.Activities, v)
	case Tag:
		c.Tags = append(c.Tags, v)
	case AbsenceType:
		c.AbsenceTypes = append(c.AbsenceTypes, v)
	case Confirmation:
		c.Confirmations = append(c.Confirmations, v)
	case Adjustment:
		c.Adjustments = append(c.Adjustments, v)
	case PeriodClose:
		c.PeriodCloses = append(c.PeriodCloses, v)
	case Holiday:
		c.Holidays = append(c.Holidays, v)
	case LeaveNotice:
		c.LeaveNotices = append(c.LeaveNotices, v)
	case PurgeLog:
		c.PurgeLogs = append(c.PurgeLogs, v)
	}
}

// DecodeRecord turns the JSON of a stored record of the given kind back into the record.
func DecodeRecord(kind Kind, data []byte) (Record, error) {
	var r Record
	var err error
	switch kind {
	case RecEmployer:
		r, err = decode[Employer](data)
	case RecModeChange:
		r, err = decode[ModeChange](data)
	case RecSchedule:
		r, err = decode[Schedule](data)
	case RecPayRule:
		r, err = decode[PayRule](data)
	case RecClient:
		r, err = decode[Client](data)
	case RecProject:
		r, err = decode[Project](data)
	case RecActivity:
		r, err = decode[Activity](data)
	case RecTag:
		r, err = decode[Tag](data)
	case RecAbsenceType:
		r, err = decode[AbsenceType](data)
	case RecConfirmation:
		r, err = decode[Confirmation](data)
	case RecAdjustment:
		r, err = decode[Adjustment](data)
	case RecPeriodClose:
		r, err = decode[PeriodClose](data)
	case RecHoliday:
		r, err = decode[Holiday](data)
	case RecLeaveNotice:
		r, err = decode[LeaveNotice](data)
	case RecPurgeLog:
		r, err = decode[PurgeLog](data)
	default:
		return nil, invalid("record kind %q", kind)
	}
	return r, err
}

func decode[T Record](data []byte) (Record, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// Employer returns the employer with the ID.
func (c Catalog) Employer(id string) (Employer, bool) {
	for _, e := range c.Employers {
		if e.ID == id {
			return e, true
		}
	}
	return Employer{}, false
}

// AbsenceTypesFor returns the built-in absence types with the owner's own types laid over them by key: for a private
// employer the owner-wide types and those of the employer, for a managed employer only those of the employer. An
// empty employerID means the owner-wide set.
func (c Catalog) AbsenceTypesFor(employerID string) []AbsenceType {
	out := DefaultAbsenceTypes()
	emp, _ := c.Employer(employerID)
	for _, t := range c.AbsenceTypes {
		if t.EmployerID != "" && t.EmployerID != employerID {
			continue
		}
		if t.EmployerID == "" && emp.Managed() {
			continue
		}
		if i := slices.IndexFunc(out, func(x AbsenceType) bool { return x.Key == t.Key }); i >= 0 {
			out[i] = t
		} else {
			out = append(out, t)
		}
	}
	return out
}

// appliesTo reports whether a record bound to recordEmployer ("" for every employer of the owner) counts for emp: a
// record for every employer only counts for private employers.
func appliesTo(emp Employer, recordEmployer string) bool {
	if recordEmployer == "" {
		return !emp.Managed()
	}
	return recordEmployer == emp.ID
}

// Locked reports whether the day of the employer lies in a closed period.
func (c Catalog) Locked(employerID string, d Date) bool {
	for _, p := range c.PeriodCloses {
		if (p.EmployerID == employerID || employerID == "") && d.Within(p.From, p.To) {
			return true
		}
	}
	return false
}

// Change says who changes a time record and why. Both go into the correction history.
type Change struct {
	By     string
	Reason string
	At     time.Time // zero means now
}

// When returns the time of the change.
func (c Change) When() time.Time {
	if c.At.IsZero() {
		return time.Now().UTC()
	}
	return c.At.UTC()
}

// Operation is what a revision records.
type Operation string

const (
	OpCreate Operation = "create"
	OpUpdate Operation = "update"
	OpDelete Operation = "delete"
)

// RecordType is the type of a time record that carries a history.
type RecordType string

const (
	RecordEntry   RecordType = "entry"
	RecordAbsence RecordType = "absence"
)

// Revision is one step in the correction history of an entry or absence. Before is the state that was replaced (empty
// for a creation), After the new state (the deleted state for a deletion). Revisions are never changed.
type Revision struct {
	ID       string          `json:"id"`
	Owner    string          `json:"owner"`
	Type     RecordType      `json:"type"`
	RecordID string          `json:"record_id"`
	Version  int             `json:"version"`
	Op       Operation       `json:"op"`
	At       time.Time       `json:"at"`
	By       string          `json:"by,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Before   json.RawMessage `json:"before,omitempty"`
	After    json.RawMessage `json:"after"`
}

// EntryFilter selects entries of one owner. Zero fields do not filter.
type EntryFilter struct {
	Owner          string
	EmployerID     string
	From, To       Date
	IncludeDeleted bool
}

// AbsenceFilter selects absences of one owner that overlap From to To. EmployerID also matches absences for every
// employer (empty EmployerID).
type AbsenceFilter struct {
	Owner          string
	EmployerID     string
	From, To       Date
	IncludeDeleted bool
}

// Errors of the stores.
var (
	// ErrNotFound: no such record for this owner.
	ErrNotFound = errors.New("not found")
	// ErrConflict: the record changed since it was read (the version does not match).
	ErrConflict = errors.New("changed in the meantime")
	// ErrLocked: the day lies in a closed period.
	ErrLocked = errors.New("period is closed")
)

// Store keeps the data of EightPerDay. Implementations: core/memstore (tests) and core/pgstore (PostgreSQL).
//
// Time records (entries, absences) are never overwritten: every change writes a Revision with the previous state, and
// a deletion only marks the record. Changes in a closed period fail with ErrLocked, stale versions with ErrConflict.
type Store interface {
	// Put inserts or replaces a catalog record.
	Put(ctx context.Context, r Record) error
	// Delete removes a catalog record. Period closes and purge logs cannot be deleted (ErrLocked).
	Delete(ctx context.Context, kind Kind, owner, id string) error
	// Catalog returns every catalog record of the owner.
	Catalog(ctx context.Context, owner string) (Catalog, error)

	// CreateEntry stores a new entry with version 1. An empty ID gets a new one.
	CreateEntry(ctx context.Context, e Entry, c Change) (Entry, error)
	// UpdateEntry replaces an entry whose stored version equals e.Version, and returns it with the next version.
	UpdateEntry(ctx context.Context, e Entry, c Change) (Entry, error)
	// DeleteEntry marks an entry as deleted.
	DeleteEntry(ctx context.Context, owner, id string, version int, c Change) error
	// Entry returns one entry, also a deleted one.
	Entry(ctx context.Context, owner, id string) (Entry, error)
	// Entries returns the entries that match, ordered by date and start.
	Entries(ctx context.Context, f EntryFilter) ([]Entry, error)

	CreateAbsence(ctx context.Context, a Absence, c Change) (Absence, error)
	UpdateAbsence(ctx context.Context, a Absence, c Change) (Absence, error)
	DeleteAbsence(ctx context.Context, owner, id string, version int, c Change) error
	Absences(ctx context.Context, f AbsenceFilter) ([]Absence, error)

	// Revisions returns the correction history of one record, or of every record of the owner if recordID is empty,
	// oldest first.
	Revisions(ctx context.Context, owner, recordID string) ([]Revision, error)

	// Purge removes time records (with their revisions) of the class dated before cutoff, for one owner or for every
	// owner if owner is empty, except those of employers on hold. It logs the run and returns the number removed.
	Purge(ctx context.Context, owner string, class Class, cutoff Date) (int, error)
	// Erase removes everything private of the owner, for the end of an account after the export: private employers
	// with all their records, and the owner-wide records. The records of managed employers stay with their
	// organization.
	Erase(ctx context.Context, owner string) error

	// PutOrganization inserts or replaces an organization. Organizations are not part of any person's catalog.
	PutOrganization(ctx context.Context, o Organization) error
	// Organization returns one organization.
	Organization(ctx context.Context, id string) (Organization, error)
	// PutMembership inserts or replaces a membership. Memberships are ended (Until), never deleted.
	PutMembership(ctx context.Context, m Membership) error
	// Memberships returns the memberships of an organization, of a person, or of both (empty means any).
	Memberships(ctx context.Context, organization, person string) ([]Membership, error)

	// Managed returns what the organization may see: its managed employers (of every owner), the catalog records,
	// entries and absences bound to them in the range, and nothing else. Private records never appear, also not
	// absences or holidays for every employer.
	Managed(ctx context.Context, organization string, from, to Date) (Data, error)
}

// Data is what the calculations work on: the owner's catalog and the time records of a range.
type Data struct {
	Catalog  Catalog
	Entries  []Entry
	Absences []Absence
}

// Load reads the data of an owner for the days from to to. The catalog is complete; entries and absences cover the
// range.
func Load(ctx context.Context, s Store, owner string, from, to Date) (Data, error) {
	cat, err := s.Catalog(ctx, owner)
	if err != nil {
		return Data{}, err
	}
	entries, err := s.Entries(ctx, EntryFilter{Owner: owner, From: from, To: to})
	if err != nil {
		return Data{}, err
	}
	absences, err := s.Absences(ctx, AbsenceFilter{Owner: owner, From: from, To: to})
	if err != nil {
		return Data{}, err
	}
	return Data{Catalog: cat, Entries: entries, Absences: absences}, nil
}
