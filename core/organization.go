// SPDX-License-Identifier: MIT

package core

import (
	"strings"
	"time"
)

// Organization is a company (or any body) that manages the time tracking of its members: it sets their schedules and
// rules, and the records of its managed employers belong to it. In UnderDoc a tenant maps to one organization. A
// person can be a member of several organizations and keep private employers next to them.
type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// TimeZone and Region are the defaults for the employers of new members.
	TimeZone string `json:"time_zone,omitempty"`
	Region   string `json:"region,omitempty"`
	// Features are the company features; all of them are off unless switched on (see CompanyFeatures).
	Features CompanyFeatures `json:"features"`
	// Retention overrides DefaultRetention per class; every value has at least MinimumYears.
	Retention []Retention `json:"retention,omitempty"`
	// Templates are the schedules the organization offers its members, by name.
	Templates []ScheduleTemplate `json:"templates,omitempty"`
}

// CompanyFeatures are the functions that go beyond recording one's own time. They are off by default, and switching
// any of them on needs the reference to a works agreement (or the statement that none is needed) and who confirmed it
// when (Art. 25 GDPR, § 87 Abs. 1 Nr. 6 BetrVG; see the legal notes). There is deliberately no feature for
// performance, location or ranking.
type CompanyFeatures struct {
	// Approval: entries and absences of members need the approval of an administrator.
	Approval bool `json:"approval,omitempty"`
	// SupervisorView: administrators see whether members are present or absent today, without history or reasons.
	SupervisorView bool `json:"supervisor_view,omitempty"`
	// Agreement names the works agreement (date, version, scope) the features rest on.
	Agreement   string     `json:"agreement,omitempty"`
	ConfirmedBy string     `json:"confirmed_by,omitempty"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
}

// Any reports whether a company feature is on.
func (f CompanyFeatures) Any() bool { return f.Approval || f.SupervisorView }

// ScheduleTemplate is a schedule an organization offers, with the mode its members start in.
type ScheduleTemplate struct {
	Name          string        `json:"name"`
	Mode          Mode          `json:"mode"`
	Days          [7]DayPlan    `json:"days"`
	OvertimeBasis OvertimeBasis `json:"overtime_basis,omitempty"`
	LeaveDays     float64       `json:"leave_days,omitempty"`
}

// Role is the role of a member.
type Role string

const (
	RoleMember Role = "member"
	RoleAdmin  Role = "admin"
)

// Membership ties a person to an organization from From until Until (open if zero), through the managed employer
// that holds the person's records there. Memberships are ended, not deleted: the records stay with the organization.
type Membership struct {
	ID           string `json:"id"`
	Organization string `json:"organization"`
	Person       string `json:"person"`
	Role         Role   `json:"role"`
	EmployerID   string `json:"employer_id,omitempty"`
	From         Date   `json:"from"`
	Until        Date   `json:"until,omitempty"`
}

// Active reports whether the membership holds on the day.
func (m Membership) Active(d Date) bool { return d.Within(m.From, m.Until) }

// Validate checks the organization. Company features need the agreement and the confirmation.
func (o Organization) Validate() error {
	if err := checkRef("organization", o.ID, true); err != nil {
		return err
	}
	if err := checkName(o.Name); err != nil {
		return err
	}
	if o.Region != "" && !KnownRegion(o.Region) {
		return invalid("region %q", o.Region)
	}
	if o.TimeZone != "" {
		if _, err := time.LoadLocation(o.TimeZone); err != nil {
			return invalid("time zone %q", o.TimeZone)
		}
	}
	if o.Features.Any() && (strings.TrimSpace(o.Features.Agreement) == "" || o.Features.ConfirmedBy == "" ||
		o.Features.ConfirmedAt == nil) {
		return invalid("company features need the works agreement and who confirmed it when")
	}
	for _, r := range o.Retention {
		if err := r.Validate(); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, t := range o.Templates {
		if err := checkName(t.Name); err != nil || seen[t.Name] {
			return invalid("template name %q", t.Name)
		}
		seen[t.Name] = true
		if !t.Mode.Valid() {
			return invalid("template mode %q", t.Mode)
		}
		s := Schedule{ID: "t", Owner: "t", EmployerID: "t", ValidFrom: NewDate(2000, 1, 1), Days: t.Days,
			OvertimeBasis: t.OvertimeBasis, LeaveDays: t.LeaveDays}
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// RetentionFor returns the retention of the class: the organization's own or the default.
func (o Organization) RetentionFor(c Class) Retention {
	for _, r := range o.Retention {
		if r.Class == c {
			return r
		}
	}
	for _, r := range DefaultRetention() {
		if r.Class == c {
			return r
		}
	}
	return Retention{Class: c, YearsAfterYearEnd: MinimumYears}
}

// Validate checks the membership.
func (m Membership) Validate() error {
	if err := checkRef("id", m.ID, true); err != nil {
		return err
	}
	if err := checkRef("organization", m.Organization, true); err != nil {
		return err
	}
	if err := checkRef("person", m.Person, true); err != nil {
		return err
	}
	if err := checkRef("employer", m.EmployerID, false); err != nil {
		return err
	}
	if m.Role != RoleMember && m.Role != RoleAdmin {
		return invalid("role %q", m.Role)
	}
	if m.From.IsZero() || (!m.Until.IsZero() && m.Until.Before(m.From)) {
		return invalid("membership needs from <= until")
	}
	return nil
}

// ActorFor returns the actor a person is in an organization on a day: an administrator only with an active admin
// membership. With no organization, the person acts alone.
func ActorFor(person, organization string, memberships []Membership, today Date) Actor {
	a := Actor{Person: person}
	if organization == "" {
		return a
	}
	for _, m := range memberships {
		if m.Person == person && m.Organization == organization && m.Active(today) {
			a.Organization = organization
			if m.Role == RoleAdmin {
				a.Admin = true
			}
		}
	}
	return a
}

// Join makes the records for a new member: the membership, the managed employer that will hold the member's records
// (with the organization's time zone and region) and, if a template is named, the schedule from it. The caller stores
// them; the organization's administrator then sets the pay rules.
func Join(o Organization, person string, role Role, template string, from Date) (Membership, Employer, *Schedule, error) {
	emp := Employer{ID: NewID(), Owner: person, Name: o.Name, Kind: KindEmployer, Holder: HolderManaged,
		Organization: o.ID, Mode: ModeSchedule, TimeZone: o.TimeZone, Region: o.Region, WeekStart: time.Monday}
	var sched *Schedule
	if template != "" {
		found := false
		for _, t := range o.Templates {
			if t.Name != template {
				continue
			}
			found = true
			emp.Mode = t.Mode
			sched = &Schedule{ID: NewID(), Owner: person, EmployerID: emp.ID, ValidFrom: from, Days: t.Days,
				OvertimeBasis: t.OvertimeBasis, LeaveDays: t.LeaveDays}
		}
		if !found {
			return Membership{}, Employer{}, nil, invalid("no template %q", template)
		}
	}
	m := Membership{ID: NewID(), Organization: o.ID, Person: person, Role: role, EmployerID: emp.ID, From: from}
	if err := m.Validate(); err != nil {
		return Membership{}, Employer{}, nil, err
	}
	if err := emp.Validate(); err != nil {
		return Membership{}, Employer{}, nil, err
	}
	return m, emp, sched, nil
}

// EndMembership ends a membership on the last day and archives its employer. The records stay with the
// organization; the person keeps a copy through ExportOwner.
func EndMembership(m Membership, emp Employer, last Date) (Membership, Employer, error) {
	if last.Before(m.From) {
		return m, emp, invalid("a membership cannot end before it starts")
	}
	m.Until = last
	emp.Archived = true
	return m, emp, nil
}
