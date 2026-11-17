// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/web/auth"
)

// Organizations manage the time tracking of their members: they set schedules and pay rules, and the records of the
// managed employers belong to them. What an organization sees comes only from core.Store.Managed: never anything
// private of its members.

type orgsData struct {
	Orgs      []orgMembership
	CanCreate bool
	Regions   []string
}

type orgMembership struct {
	Org        core.Organization
	Membership core.Membership
	Admin      bool
}

type orgData struct {
	Org        core.Organization
	Today      core.Date
	Members    []memberView
	Candidates []*auth.Account
	Modes      []core.Mode
	Regions    []string
	Weekdays   []int
	Presence   []presenceView
	// ShowPresence: the company feature is on, so the page shows who is present today.
	ShowPresence bool
	Back         string
}

type memberView struct {
	core.Membership
	Name   string
	Active bool
}

type presenceView struct {
	Name    string
	Present bool
	Absent  bool
}

// orgsPage lists the organizations of the person; an administrator of the program may found one.
func (h *handler) orgsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account := accountFrom(ctx)
	memberships, err := h.options.Store.Memberships(ctx, "", account.ID)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	d := orgsData{CanCreate: account.Admin, Regions: core.Regions()}
	today := h.today(nil)
	seen := map[string]bool{}
	for _, m := range memberships {
		if seen[m.Organization] || !m.Active(today) {
			continue
		}
		seen[m.Organization] = true
		org, err := h.options.Store.Organization(ctx, m.Organization)
		if err != nil {
			h.failed(w, r, err)
			return
		}
		d.Orgs = append(d.Orgs, orgMembership{Org: org, Membership: m,
			Admin: core.ActorFor(account.ID, org.ID, memberships, today).Admin})
	}
	h.render(w, r, http.StatusOK, "orgs", view{Title: h.t(r, "orgs.title"), Data: d})
}

// createOrg founds an organization; its founder administers it, without records of their own there.
func (h *handler) createOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account := accountFrom(ctx)
	if !account.Admin {
		h.message(w, r, http.StatusForbidden, h.t(r, "error.forbidden.title"), h.t(r, "error.forbidden.text"))
		return
	}
	org := core.Organization{ID: core.NewID(), Name: strings.TrimSpace(r.PostFormValue("name")), TimeZone: core.DefaultTimeZone,
		Region: r.PostFormValue("region")}
	if err := org.Validate(); err != nil {
		h.back(w, r, err, "")
		return
	}
	if err := h.options.Store.PutOrganization(ctx, org); err != nil {
		h.back(w, r, err, "")
		return
	}
	m := core.Membership{ID: core.NewID(), Organization: org.ID, Person: account.ID, Role: core.RoleAdmin, From: h.today(nil)}
	if err := h.options.Store.PutMembership(ctx, m); err != nil {
		h.back(w, r, err, "")
		return
	}
	http.Redirect(w, r, "/orgs/"+org.ID+"?done=created", http.StatusSeeOther)
}

// orgAdmin loads the organization of the address and lets only its active administrators through.
func (h *handler) orgAdmin(w http.ResponseWriter, r *http.Request) (core.Organization, []core.Membership, bool) {
	ctx := r.Context()
	org, err := h.options.Store.Organization(ctx, r.PathValue("id"))
	if errors.Is(err, core.ErrNotFound) {
		h.notFound(w, r)
		return org, nil, false
	}
	if err != nil {
		h.failed(w, r, err)
		return org, nil, false
	}
	members, err := h.options.Store.Memberships(ctx, org.ID, "")
	if err != nil {
		h.failed(w, r, err)
		return org, nil, false
	}
	if !core.ActorFor(accountFrom(ctx).ID, org.ID, members, h.today(nil)).Admin {
		h.notFound(w, r) // an organization is not even named to someone who does not administer it
		return org, nil, false
	}
	return org, members, true
}

func (h *handler) orgPage(w http.ResponseWriter, r *http.Request) {
	org, members, ok := h.orgAdmin(w, r)
	if !ok {
		return
	}
	d, err := h.orgData(r, org, members)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "org", view{Title: org.Name, Data: d})
}

func (h *handler) orgData(r *http.Request, org core.Organization, members []core.Membership) (orgData, error) {
	ctx := r.Context()
	today := h.today(nil)
	d := orgData{Org: org, Today: today, Modes: core.Modes, Regions: core.Regions(), Weekdays: weekdays, Back: "/orgs/" + org.ID}
	accounts, err := h.options.Auth.Accounts(ctx)
	if err != nil {
		return d, err
	}
	names := map[string]string{}
	for _, a := range accounts {
		names[a.ID] = a.DisplayName + " (" + a.Username + ")"
	}
	active := map[string]bool{}
	for _, m := range members {
		v := memberView{Membership: m, Name: names[m.Person], Active: m.Active(today)}
		if v.Active {
			active[m.Person] = true
		}
		d.Members = append(d.Members, v)
	}
	slices.SortFunc(d.Members, func(a, b memberView) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	for _, a := range accounts {
		if !active[a.ID] && !a.Disabled {
			d.Candidates = append(d.Candidates, a)
		}
	}
	// The supervisor view: present or absent today, without reasons and without history, only when switched on.
	if org.Features.SupervisorView {
		d.ShowPresence = true
		data, err := h.options.Store.Managed(ctx, org.ID, today, today)
		if err != nil {
			return d, err
		}
		for _, m := range d.Members {
			if !m.Active || m.EmployerID == "" {
				continue
			}
			p := presenceView{Name: m.Name}
			for _, a := range data.Absences {
				if a.EmployerID == m.EmployerID && a.DeletedAt == nil && today.Within(a.From, a.To) {
					p.Absent = true
				}
			}
			for _, e := range data.Entries {
				if e.EmployerID == m.EmployerID && e.DeletedAt == nil && e.Date == today && e.Kind == core.EntryWork {
					p.Present = !p.Absent
				}
			}
			d.Presence = append(d.Presence, p)
		}
	}
	return d, nil
}

// changeOrg acts on an organization: its details, members, templates and company features.
func (h *handler) changeOrg(w http.ResponseWriter, r *http.Request) {
	org, members, ok := h.orgAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	account := accountFrom(ctx)
	var err error
	switch r.PathValue("action") {
	case "details":
		org.Name = strings.TrimSpace(r.PostFormValue("name"))
		org.Region = r.PostFormValue("region")
		if err = org.Validate(); err == nil {
			err = h.options.Store.PutOrganization(ctx, org)
		}
	case "member":
		err = h.addMember(r, org, members)
	case "role":
		err = h.setRole(r, org, members)
	case "end":
		err = h.endMember(r, members)
	case "template":
		t := core.ScheduleTemplate{Name: strings.TrimSpace(r.PostFormValue("name")), Mode: core.Mode(r.PostFormValue("mode")),
			OvertimeBasis: core.OvertimeBasis(r.PostFormValue("basis"))}
		for _, day := range weekdays {
			minutes, perr := parseHours(r.PostFormValue("hours-" + strconv.Itoa(day)))
			if perr != nil || minutes < 0 {
				err = core.ErrInvalid
			}
			t.Days[day].Minutes = minutes
		}
		if raw := strings.TrimSpace(r.PostFormValue("leave_days")); raw != "" && err == nil {
			t.LeaveDays, err = strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		}
		if err == nil {
			org.Templates = append(org.Templates, t)
			if err = org.Validate(); err == nil {
				err = h.options.Store.PutOrganization(ctx, org)
			}
		}
	case "remove-template":
		org.Templates = slices.DeleteFunc(org.Templates, func(t core.ScheduleTemplate) bool { return t.Name == r.PostFormValue("name") })
		err = h.options.Store.PutOrganization(ctx, org)
	case "features":
		features := core.CompanyFeatures{SupervisorView: r.PostFormValue("supervisor_view") == "on",
			Agreement: strings.TrimSpace(r.PostFormValue("agreement"))}
		if features.Any() {
			// Switching on needs the reference and the confirmation of the person who does it (A1, A5, A11).
			if r.PostFormValue("confirm") != "on" || features.Agreement == "" {
				h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "org.features.needconfirm"))
				return
			}
			now := h.now()
			features.ConfirmedBy, features.ConfirmedAt = account.ID, &now
		} else {
			features = core.CompanyFeatures{}
		}
		org.Features = features
		if err = org.Validate(); err == nil {
			err = h.options.Store.PutOrganization(ctx, org)
		}
	default:
		h.notFound(w, r)
		return
	}
	h.back(w, r, err, "saved")
}

// addMember lets an account join: the membership, the managed employer that will hold the member's records, and the
// schedule from a template. An administrator who does not record time there gets the role without an employer.
func (h *handler) addMember(r *http.Request, org core.Organization, members []core.Membership) error {
	ctx := r.Context()
	person := r.PostFormValue("person")
	if _, err := h.options.Auth.Account(ctx, person); err != nil {
		return core.ErrNotFound
	}
	today := h.today(nil)
	for _, m := range members {
		if m.Person == person && m.Active(today) {
			return core.ErrConflict
		}
	}
	from, err := formDate(r, "from")
	if err != nil {
		from = today
	}
	role := core.Role(r.PostFormValue("role"))
	if r.PostFormValue("records") != "on" {
		m := core.Membership{ID: core.NewID(), Organization: org.ID, Person: person, Role: role, From: from}
		if err := m.Validate(); err != nil {
			return err
		}
		return h.options.Store.PutMembership(ctx, m)
	}
	m, emp, schedule, err := core.Join(org, person, role, r.PostFormValue("template"), from)
	if err != nil {
		return err
	}
	if err := h.options.Store.Put(ctx, emp); err != nil {
		return err
	}
	if schedule != nil {
		if err := h.options.Store.Put(ctx, *schedule); err != nil {
			return err
		}
	}
	return h.options.Store.PutMembership(ctx, m)
}

func findMembership(members []core.Membership, id string) (core.Membership, bool) {
	for _, m := range members {
		if m.ID == id {
			return m, true
		}
	}
	return core.Membership{}, false
}

// setRole makes a member an administrator or takes it; the last active administrator keeps it.
func (h *handler) setRole(r *http.Request, org core.Organization, members []core.Membership) error {
	m, ok := findMembership(members, r.PostFormValue("membership"))
	if !ok {
		return core.ErrNotFound
	}
	m.Role = core.Role(r.PostFormValue("role"))
	if err := m.Validate(); err != nil {
		return err
	}
	if m.Role != core.RoleAdmin && h.lastAdmin(members, m.ID) {
		return errLastOrgAdmin
	}
	return h.options.Store.PutMembership(r.Context(), m)
}

var errLastOrgAdmin = errors.New("the organization would have no administrator")

func (h *handler) lastAdmin(members []core.Membership, except string) bool {
	today := h.today(nil)
	for _, m := range members {
		if m.ID != except && m.Role == core.RoleAdmin && m.Active(today) {
			return false
		}
	}
	return true
}

// endMember ends a membership on a day and archives its employer; the records stay with the organization.
func (h *handler) endMember(r *http.Request, members []core.Membership) error {
	ctx := r.Context()
	m, ok := findMembership(members, r.PostFormValue("membership"))
	if !ok {
		return core.ErrNotFound
	}
	if m.Role == core.RoleAdmin && h.lastAdmin(members, m.ID) {
		return errLastOrgAdmin
	}
	last, err := formDate(r, "until")
	if err != nil {
		last = h.today(nil)
	}
	if m.EmployerID == "" {
		m.Until = last
		return h.options.Store.PutMembership(ctx, m)
	}
	cat, err := h.options.Store.Catalog(ctx, m.Person)
	if err != nil {
		return err
	}
	emp, found := cat.Employer(m.EmployerID)
	if !found {
		return core.ErrNotFound
	}
	m, emp, err = core.EndMembership(m, emp, last)
	if err != nil {
		return err
	}
	if err := h.options.Store.Put(ctx, emp); err != nil {
		return err
	}
	return h.options.Store.PutMembership(ctx, m)
}
