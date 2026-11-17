// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// AccessAction is what an access did with a person's data.
type AccessAction string

const (
	AccessRead   AccessAction = "read"
	AccessChange AccessAction = "change"
	AccessExport AccessAction = "export"
	AccessDecide AccessAction = "decide"
	// AccessRole: a role or membership of the person changed.
	AccessRole AccessAction = "role"
)

// Access is one entry of the access log: who (Actor, for Organization) did what (Action, What, the days From to To)
// with the data of whom (Subject), and when. The log of a subject is a chain: every entry holds the hash of the one
// before (Prev) and its own (Hash), so a changed or removed entry in the middle shows. Stores only ever add entries
// and remove them by the retention purge (ClassAccessLog).
//
// The chain shows changes of single entries; it does not stop someone who rewrites the whole chain of a subject.
// Whoever needs that keeps the last hash somewhere else as well.
type Access struct {
	ID           string       `json:"id"`
	At           time.Time    `json:"at"`
	Actor        string       `json:"actor"`
	Organization string       `json:"organization,omitempty"`
	Subject      string       `json:"subject"`
	EmployerID   string       `json:"employer_id,omitempty"`
	Action       AccessAction `json:"action"`
	// What names the data, such as "entries" or "absence 1f3c", without the data itself.
	What string `json:"what"`
	From Date   `json:"from,omitempty"`
	To   Date   `json:"to,omitempty"`
	Prev string `json:"prev,omitempty"`
	Hash string `json:"hash"`
}

// AccessLog keeps the access log. It is separate from Store, so a store of one's own keeps working without it; both
// stores of this module have it.
type AccessLog interface {
	// LogAccess adds an entry: it fills ID and At if they are empty, links it to the subject's last entry and returns
	// it sealed.
	LogAccess(ctx context.Context, a Access) (Access, error)
	// Accesses returns the log of one subject, oldest first, for the subject to see.
	Accesses(ctx context.Context, subject string) ([]Access, error)
}

// Validate checks the entry, without the seal.
func (a Access) Validate() error {
	if err := checkRef("id", a.ID, true); err != nil {
		return err
	}
	if err := checkRef("actor", a.Actor, true); err != nil {
		return err
	}
	if err := checkRef("subject", a.Subject, true); err != nil {
		return err
	}
	if err := checkRef("organization", a.Organization, false); err != nil {
		return err
	}
	if err := checkRef("employer", a.EmployerID, false); err != nil {
		return err
	}
	if a.At.IsZero() {
		return invalid("access without time")
	}
	switch a.Action {
	case AccessRead, AccessChange, AccessExport, AccessDecide, AccessRole:
	default:
		return invalid("access action %q", a.Action)
	}
	if strings.TrimSpace(a.What) == "" || len(a.What) > 500 {
		return invalid("an access names what it touched, in at most 500 characters")
	}
	if !a.From.IsZero() && !a.To.IsZero() && a.To.Before(a.From) {
		return invalid("access needs from <= to")
	}
	return nil
}

// digest is the hash of the entry with everything but Hash.
func (a Access) digest() (string, error) {
	a.Hash = ""
	b, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// PrepareAccess is the shared part of AccessLog.LogAccess: it fills ID and At (whole microseconds, as PostgreSQL keeps
// them), validates the entry and seals it after prev, the hash of the subject's last entry ("" for the first).
func PrepareAccess(a Access, prev string) (Access, error) {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.At.IsZero() {
		a.At = time.Now()
	}
	a.At = a.At.UTC().Truncate(time.Microsecond)
	a.Prev, a.Hash = prev, ""
	if err := a.Validate(); err != nil {
		return Access{}, err
	}
	h, err := a.digest()
	if err != nil {
		return Access{}, err
	}
	a.Hash = h
	return a, nil
}

// ErrTampered: the access log does not hold together.
var ErrTampered = errors.New("access log changed")

// VerifyAccesses checks the chain of one subject's log, oldest first: every hash matches its entry, and every entry
// points to the one before. The first entry may point to one that the retention purge removed.
func VerifyAccesses(log []Access) error {
	for i, a := range log {
		h, err := a.digest()
		if err != nil {
			return err
		}
		if h != a.Hash {
			return fmt.Errorf("%w: entry %s does not match its hash", ErrTampered, a.ID)
		}
		if i > 0 && (a.Prev != log[i-1].Hash || a.Subject != log[i-1].Subject) {
			return fmt.Errorf("%w: entry %s does not follow %s", ErrTampered, a.ID, log[i-1].ID)
		}
	}
	return nil
}

// LogAccessOnce adds a to the log like LogAccess, unless the subject's log already holds an entry of the same day (in
// loc; UTC if nil) by the same actor and organization, for the same employer, action and What, whose days cover a's.
// It returns the entry that stands for the access, and whether it was added now. Pages that are opened again and again
// (an organization's overview) log their reads with it: one entry per view, person and day instead of one per call.
//
// Nothing that was read goes unlogged: a read of other days, of another view or by someone else still adds an entry.
// The chain is not touched, entries are only ever added, so VerifyAccesses finds a changed or removed entry as before.
// Two calls at the same moment may both add one.
func LogAccessOnce(ctx context.Context, log AccessLog, a Access, loc *time.Location) (Access, bool, error) {
	if loc == nil {
		loc = time.UTC
	}
	at := a.At
	if at.IsZero() {
		at = time.Now()
	}
	y, m, d := at.In(loc).Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	entries, err := log.Accesses(ctx, a.Subject)
	if err != nil {
		return Access{}, false, err
	}
	// The whole log is looked at: an embedding may write entries with times of its own, out of order.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.At.Before(dayStart) || !e.At.Before(dayEnd) {
			continue
		}
		if e.Actor == a.Actor && e.Organization == a.Organization && e.EmployerID == a.EmployerID &&
			e.Action == a.Action && e.What == a.What && covers(e, a) {
			return e, false, nil
		}
	}
	a.At = at
	logged, err := log.LogAccess(ctx, a)
	return logged, err == nil, err
}

// covers reports whether the days of e include those of a; an entry without days only stands for one without days.
func covers(e, a Access) bool {
	if e.From.IsZero() || e.To.IsZero() || a.From.IsZero() || a.To.IsZero() {
		return e.From == a.From && e.To == a.To
	}
	return !a.From.Before(e.From) && !a.To.After(e.To)
}

// ReadManaged is Store.Managed with the access log: it logs a read for every person whose records the organization
// gets, before it hands them over. Use it for every read of an organization that a person did not start for their own
// data; purpose goes into What.
func ReadManaged(ctx context.Context, s Store, log AccessLog, actor Actor, organization string, from, to Date,
	purpose string) (Data, error) {
	d, err := s.Managed(ctx, organization, from, to)
	if err != nil {
		return Data{}, err
	}
	for _, subject := range ownersOf(d) {
		if _, err := log.LogAccess(ctx, Access{Actor: actor.Person, Organization: organization, Subject: subject,
			Action: AccessRead, What: purpose, From: from, To: to}); err != nil {
			return Data{}, err
		}
	}
	return d, nil
}

// ownersOf returns the owners of the employers in d, sorted: the people whose records an organization's read gets.
func ownersOf(d Data) []string {
	var subjects []string
	for _, e := range d.Catalog.Employers {
		if !slices.Contains(subjects, e.Owner) {
			subjects = append(subjects, e.Owner)
		}
	}
	slices.Sort(subjects)
	return subjects
}

// ReadManagedDaily is ReadManaged for a view that is opened again and again: it logs the read for every person with
// LogAccessOnce, so the same view of the same days by the same actor is logged once a day (in loc) and not on every
// call. at is the time of the read (zero: now). ReadManaged stays as it was and logs every call.
func ReadManagedDaily(ctx context.Context, s Store, log AccessLog, actor Actor, organization string, from, to Date,
	purpose string, at time.Time, loc *time.Location) (Data, error) {
	d, err := s.Managed(ctx, organization, from, to)
	if err != nil {
		return Data{}, err
	}
	for _, subject := range ownersOf(d) {
		if _, _, err := LogAccessOnce(ctx, log, Access{At: at, Actor: actor.Person, Organization: organization,
			Subject: subject, Action: AccessRead, What: purpose, From: from, To: to}, loc); err != nil {
			return Data{}, err
		}
	}
	return d, nil
}
