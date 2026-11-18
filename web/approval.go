// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

// Approval (a company feature) and the access log. A member hands in a period or a leave request of a managed
// employer, an administrator of the organization approves or rejects it with a reason, never their own (core.Submit,
// core.Decide). Every read and change an organization makes of a person's data goes into the access log, which the
// person sees on /access.

// approvalOrg returns the organization of a managed employer and whether it has switched approval on.
func (h *handler) approvalOrg(ctx context.Context, emp core.Employer) (core.Organization, bool, error) {
	if !emp.Managed() {
		return core.Organization{}, false, nil
	}
	org, err := h.options.Store.Organization(ctx, emp.Organization)
	if err != nil {
		return org, false, err
	}
	return org, org.Features.Approval, nil
}

// submissionView is a submission as the person sees it, with what can still be done with it.
type submissionView struct {
	core.Submission
	// Name is the absence type of a leave request.
	Name string
	// Correctable: approved, and no correction asked for or granted yet.
	Correctable bool
}

// submissionsOf lists the person's submissions for an employer, newest first.
func submissionsOf(cat core.Catalog, employerID string, names map[string]string, absences []core.Absence) []submissionView {
	corrected := map[string]bool{}
	for _, s := range cat.Submissions {
		if s.Kind == core.SubmitCorrection && (s.Status == core.SubmissionSubmitted || s.Status == core.SubmissionApproved) {
			corrected[s.Supersedes] = true
		}
	}
	types := map[string]string{}
	for _, a := range absences {
		types[a.ID] = names[a.Type]
	}
	var out []submissionView
	for _, s := range cat.Submissions {
		if s.EmployerID != employerID {
			continue
		}
		out = append(out, submissionView{Submission: s, Name: types[s.AbsenceID],
			Correctable: s.Status == core.SubmissionApproved && s.Kind != core.SubmitCorrection && !corrected[s.ID]})
	}
	slices.SortFunc(out, func(a, b submissionView) int { return b.SubmittedAt.Compare(a.SubmittedAt) })
	return out
}

// submit hands in a period of a managed employer, or asks to correct an approved submission.
func (h *handler) submit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	// Without approval on, core.Submit refuses.
	org, _, err := h.approvalOrg(ctx, emp)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	req := core.SubmitRequest{Kind: core.SubmissionKind(r.PostFormValue("kind")), EmployerID: emp.ID,
		Supersedes: r.PostFormValue("supersedes"), Note: strings.TrimSpace(r.PostFormValue("note")), At: h.now()}
	switch req.Kind {
	case core.SubmitPeriod:
		if req.From, err = formDate(r, "from"); err == nil {
			req.To, err = formDate(r, "to")
		}
		if err != nil {
			h.back(w, r, err, "")
			return
		}
	case core.SubmitCorrection:
		if req.Note == "" {
			h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "approval.correction.why"))
			return
		}
	default:
		h.notFound(w, r)
		return
	}
	data := core.Data{Catalog: cat}
	if req.Kind == core.SubmitPeriod {
		if data, err = core.Load(ctx, h.options.Store, emp.Owner, req.From, req.To); err != nil {
			h.failed(w, r, err)
			return
		}
	}
	s, err := core.Submit(org, data, req)
	if err == nil {
		err = h.options.Store.Put(ctx, s)
	}
	h.back(w, r, err, "submitted")
}

// requestLeave hands in a leave request for an absence that was just booked as requested.
func (h *handler) requestLeave(ctx context.Context, org core.Organization, cat core.Catalog, a core.Absence) error {
	s, err := core.Submit(org, core.Data{Catalog: cat, Absences: []core.Absence{a}},
		core.SubmitRequest{Kind: core.SubmitLeave, EmployerID: a.EmployerID, AbsenceID: a.ID, At: h.now()})
	if err != nil {
		return err
	}
	return h.options.Store.Put(ctx, s)
}

// createAbsence books an absence. On a managed employer whose organization approves leave, an absence that deducts
// leave or needs approval is booked as requested and handed in at once.
func (h *handler) createAbsence(r *http.Request, cat core.Catalog, emp core.Employer, a core.Absence) error {
	ctx := r.Context()
	org, on, err := h.approvalOrg(ctx, emp)
	if err != nil {
		return err
	}
	needs := false
	for _, t := range cat.AbsenceTypesFor(emp.ID) {
		if t.Key == a.Type && (t.DeductsLeave || t.NeedsApproval) {
			needs = true
		}
	}
	if on && needs {
		a.Status = core.StatusRequested
	}
	created, err := h.options.Store.CreateAbsence(ctx, a, change(r, ""))
	if err != nil || created.Status != core.StatusRequested {
		return err
	}
	return h.requestLeave(ctx, org, cat, created)
}

// withdraw takes back an open submission of the person.
func (h *handler) withdraw(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	for _, s := range cat.Submissions {
		if s.ID == r.PathValue("id") {
			withdrawn, err := core.Withdraw(s, accountFrom(ctx).ID, h.now())
			if err == nil {
				err = h.options.Store.Put(ctx, withdrawn)
			}
			h.back(w, r, err, "withdrawn")
			return
		}
	}
	h.back(w, r, core.ErrNotFound, "")
}

// reviewView is an open submission as an administrator sees it.
type reviewView struct {
	core.Submission
	Member string
	// Absence is the absence a leave request asks for.
	Absence *core.Absence
	Type    string
	// Own: the administrator handed it in; another administrator decides it (core.Decide refuses one's own).
	Own bool
	// Changes are the days a change request asks for, as "Mon 11-16: 6".
	Changes []string
}

// orgLocation is the time zone of an organization, which decides its days.
func orgLocation(org core.Organization) *time.Location {
	name := org.TimeZone
	if name == "" {
		name = core.DefaultTimeZone
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

// reviews lists the open submissions of the organization's members, oldest first, from the data a reviewer may see
// (core.ReviewData). The days read follow the open submissions (core.ReviewRange), however far from today they lie.
// The read is logged for every member once a day (core.ReadManagedDaily): opening the page again adds nothing.
func (h *handler) reviews(r *http.Request, org core.Organization, actor core.Actor, names map[string]string) ([]reviewView, error) {
	ctx := r.Context()
	today := h.today(nil)
	// The catalog names the open submissions; the records of today read with it lie inside the range logged below.
	open, err := h.options.Store.Managed(ctx, org.ID, today, today)
	if err != nil {
		return nil, err
	}
	from, to := core.ReviewRange(open.Catalog, today)
	data, err := core.ReadManagedDaily(ctx, h.options.Store, h.options.AccessLog, actor, org.ID, from, to, "approval",
		h.now(), orgLocation(org))
	if err != nil {
		return nil, err
	}
	data = core.ReviewData(org, data)
	types := map[string]string{}
	for _, t := range core.DefaultAbsenceTypes() {
		types[t.Key] = h.t(r, "absence."+t.Key)
	}
	for _, t := range data.Catalog.AbsenceTypes {
		if _, builtIn := types[t.Key]; !builtIn {
			types[t.Key] = t.Name
		}
	}
	var out []reviewView
	for _, s := range data.Catalog.Submissions {
		if s.Status != core.SubmissionSubmitted {
			continue
		}
		v := reviewView{Submission: s, Member: names[s.Owner], Own: s.Owner == actor.Person}
		for _, p := range s.Proposal {
			v.Changes = append(v.Changes, h.t(r, "weekday."+strconv.Itoa(int(p.Date.Weekday())))+" "+shortDate(h.lang(r), p.Date)+": "+
				h.proposedText(r, data.Catalog.AbsenceTypes, p)+noteSuffix(p.Note))
		}
		for i, a := range data.Absences {
			if a.ID == s.AbsenceID && a.Owner == s.Owner {
				v.Absence, v.Type = &data.Absences[i], types[a.Type]
			}
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b reviewView) int { return a.SubmittedAt.Compare(b.SubmittedAt) })
	return out, nil
}

var errReasonNeeded = errors.New("a rejection needs a reason")

// decide approves or rejects an open submission of a member; a rejection needs a reason. A decided leave request also
// writes the decision into the absence. The decision goes into the member's access log.
func (h *handler) decide(r *http.Request, org core.Organization, actor core.Actor) error {
	ctx := r.Context()
	approve := r.PostFormValue("decision") == "approve"
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if !approve && reason == "" {
		return errReasonNeeded
	}
	today := h.today(nil)
	data, err := h.options.Store.Managed(ctx, org.ID, today, today)
	if err != nil {
		return err
	}
	var sub core.Submission
	for _, s := range data.Catalog.Submissions {
		if s.ID == r.PostFormValue("submission") {
			sub = s
		}
	}
	emp, found := data.Catalog.Employer(sub.EmployerID)
	if sub.ID == "" || !found {
		return core.ErrNotFound
	}
	// The records of the submission's own days, however far from today they lie.
	if sub.Kind == core.SubmitLeave {
		if data, err = h.options.Store.Managed(ctx, org.ID, sub.From, sub.To); err != nil {
			return err
		}
	}
	decided, err := core.Decide(sub, emp, actor, approve, reason, h.now())
	if err != nil {
		return err
	}
	// A leave request writes its decision into the absence too: found first, so neither is written without the other.
	var leave *core.Absence
	if decided.Kind == core.SubmitLeave {
		for i, a := range data.Absences {
			if a.ID == decided.AbsenceID && a.Owner == decided.Owner {
				leave = &data.Absences[i]
			}
		}
		if leave == nil {
			return core.ErrNotFound
		}
	}
	if err := h.options.Store.Put(ctx, decided); err != nil {
		return err
	}
	if decided.Kind == core.SubmitCorrection && decided.Status == core.SubmissionApproved && len(decided.Proposal) > 0 {
		if err := h.applyChange(r, org, actor, emp, decided); err != nil {
			return err
		}
	}
	if leave != nil {
		updated, err := core.LeaveDecision(decided, *leave)
		if err != nil {
			return err
		}
		if _, err := h.options.Store.UpdateAbsence(ctx, updated, core.Change{By: actor.Person, Reason: reason}); err != nil {
			return err
		}
	}
	return h.logAccess(ctx, actor.Person, org.ID, decided.Owner, emp.ID, core.AccessDecide,
		"submission "+string(decided.Kind)+" "+string(decided.Status), decided.From, decided.To)
}

// logAccess records what someone did with another person's data. A person's own doings are not logged.
func (h *handler) logAccess(ctx context.Context, actor, organization, subject, employerID string, action core.AccessAction,
	what string, from, to core.Date) error {
	if actor == subject {
		return nil
	}
	_, err := h.options.AccessLog.LogAccess(ctx, core.Access{At: h.now(), Actor: actor, Organization: organization, Subject: subject,
		EmployerID: employerID, Action: action, What: what, From: from, To: to})
	return err
}

// accessData is the person's access log, newest first, with the names of who acted and for which organization.
type accessData struct {
	Accesses []accessView
	// Intact: the chain of the log holds together (core.VerifyAccesses).
	Intact bool
}

type accessView struct {
	core.Access
	ActorName string
	OrgName   string
	// When is the time of the access in the program's time zone.
	When string
}

// accessPage shows the person who read, changed or decided what of their data, and when.
func (h *handler) accessPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log, err := h.options.AccessLog.Accesses(ctx, accountFrom(ctx).ID)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	d := accessData{Intact: core.VerifyAccesses(log) == nil}
	accounts, err := h.options.Auth.Accounts(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	names := map[string]string{}
	for _, a := range accounts {
		names[a.ID] = a.DisplayName
	}
	orgs := map[string]string{}
	loc, err := time.LoadLocation(core.DefaultTimeZone)
	if err != nil {
		loc = time.UTC
	}
	lang := h.lang(r)
	for i := len(log) - 1; i >= 0; i-- {
		a := log[i]
		at := a.At.In(loc)
		v := accessView{Access: a, ActorName: names[a.Actor], When: formatDate(lang, at) + " " + at.Format("15:04")}
		if a.Organization != "" {
			if _, seen := orgs[a.Organization]; !seen {
				if o, err := h.options.Store.Organization(ctx, a.Organization); err == nil {
					orgs[a.Organization] = o.Name
				}
			}
			v.OrgName = orgs[a.Organization]
		}
		d.Accesses = append(d.Accesses, v)
	}
	h.render(w, r, http.StatusOK, "access", view{Title: h.t(r, "access.title"), Data: d})
}

func noteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return " („" + note + "“)"
}
