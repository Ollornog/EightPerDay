// SPDX-License-Identifier: MIT

package core

import (
	"slices"
	"strings"
)

// CalendarAbsence is an absence as a calendar shows it: who, for which employer, what kind, which days, and whether it
// is decided. It is made for a calendar of an embedding application (UnderDoc's M-18) and carries no note.
type CalendarAbsence struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	// EmployerID is empty for an absence of every private employer; Employer is its name then empty as well.
	EmployerID string `json:"employer_id,omitempty"`
	Employer   string `json:"employer,omitempty"`
	// Type is the key of the absence type (vacation, sick, …), Name its name.
	Type    string        `json:"type"`
	Name    string        `json:"name"`
	From    Date          `json:"from"`
	To      Date          `json:"to"`
	HalfDay bool          `json:"half_day,omitempty"`
	Status  AbsenceStatus `json:"status"`
	// Leave: the days count against the leave entitlement. Health: the type reveals health data, so a calendar that
	// others see shows it only as away (see Overlaps, which leaves such types out for reviewers).
	Leave  bool `json:"leave,omitempty"`
	Health bool `json:"health,omitempty"`
}

// CalendarAbsences returns the absences of the data that overlap from to to (zero dates are open), without rejected
// and deleted ones, ordered by first day, owner and ID. It works on a person's data (Load) and on what an organization
// may see (Store.Managed); the caller decides who sees which.
func CalendarAbsences(d Data, from, to Date) []CalendarAbsence {
	out := []CalendarAbsence{}
	for _, a := range d.Absences {
		if a.DeletedAt != nil || a.Status == StatusRejected {
			continue
		}
		if (!to.IsZero() && to.Before(a.From)) || (!from.IsZero() && a.To.Before(from)) {
			continue
		}
		c := CalendarAbsence{ID: a.ID, Owner: a.Owner, EmployerID: a.EmployerID, Type: a.Type, Name: a.Type, From: a.From,
			To: a.To, HalfDay: a.HalfDay, Status: a.Status}
		if emp, ok := d.Catalog.Employer(a.EmployerID); ok {
			c.Employer = emp.Name
		}
		for _, t := range d.Catalog.AbsenceTypesFor(a.EmployerID) {
			if t.Key == a.Type {
				c.Name, c.Leave, c.Health = t.Name, t.DeductsLeave, t.Health
			}
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b CalendarAbsence) int {
		if c := strings.Compare(a.From.String(), b.From.String()); c != 0 {
			return c
		}
		if c := strings.Compare(a.Owner, b.Owner); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}
