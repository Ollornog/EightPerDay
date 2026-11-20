// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"strings"
	"time"
)

// The approval of the company features (CompanyFeatures.Approval): a member hands in a period of time or a leave
// request, an administrator of the organization approves or rejects it with a reason. Handed-in and approved records
// are locked; a correction is a new request that, once approved, opens them again. Without the feature there are no
// submissions, and nothing changes.

// SubmissionKind is what a submission hands in.
type SubmissionKind string

const (
	// SubmitPeriod hands in the days From to To of the employer.
	SubmitPeriod SubmissionKind = "period"
	// SubmitLeave asks for the absence AbsenceID (in status requested).
	SubmitLeave SubmissionKind = "leave"
	// SubmitCorrection asks to open the submission Supersedes again, which is approved; Note says why.
	SubmitCorrection SubmissionKind = "correction"
)

// SubmissionStatus is the state of a submission. Only a submitted one can change; the others are final.
type SubmissionStatus string

const (
	SubmissionSubmitted SubmissionStatus = "submitted"
	SubmissionApproved  SubmissionStatus = "approved"
	SubmissionRejected  SubmissionStatus = "rejected"
	SubmissionWithdrawn SubmissionStatus = "withdrawn"
)

// Submission is a period, a leave request or a correction handed in for approval. Stores keep it as a catalog record
// that can only be replaced while it is submitted, and never deleted (ErrLocked).
type Submission struct {
	ID         string           `json:"id"`
	Owner      string           `json:"owner"`
	EmployerID string           `json:"employer_id"`
	Kind       SubmissionKind   `json:"kind"`
	From       Date             `json:"from"`
	To         Date             `json:"to"`
	AbsenceID  string           `json:"absence_id,omitempty"`
	Supersedes string           `json:"supersedes,omitempty"`
	Status     SubmissionStatus `json:"status"`
	// Note is the person's note; a correction says why.
	Note        string    `json:"note,omitempty"`
	SubmittedAt time.Time `json:"submitted_at"`
	// Target, Actual and Balance are the minutes of a period when it was handed in.
	Target  int `json:"target,omitempty"`
	Actual  int `json:"actual,omitempty"`
	Balance int `json:"balance,omitempty"`
	// DecidedAt, DecidedBy and Reason record the decision (or the withdrawal); a rejection always has a reason.
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	DecidedBy string     `json:"decided_by,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	// Proposal is what a correction asks the days of an approved period to hold (a change request). Approving it
	// books these values and approves the period as corrected; until then the approved values count.
	Proposal []ProposedDay `json:"proposal,omitempty"`
}

// ProposedDay is what a change request asks one day to hold: hours (with their half of the day and the span typed for
// them), an absence of a type with a short code, or half a day of such an absence with hours, the note of the day, and
// the hours on projects. The hours and the note are the whole day as asked for; Projects names only the projects whose
// hours change (zero takes them off), the others stay.
type ProposedDay struct {
	Date     Date              `json:"date"`
	Minutes  int               `json:"minutes,omitempty"`
	Part     DayPart           `json:"part,omitempty"`
	Typed    string            `json:"typed,omitempty"`
	Absence  string            `json:"absence,omitempty"`
	HalfDay  bool              `json:"half_day,omitempty"`
	Note     string            `json:"note,omitempty"`
	Projects []ProposedProject `json:"projects,omitempty"`
}

// ProposedProject is the hours a change request asks a project of the employer to hold on the day.
type ProposedProject struct {
	ProjectID string `json:"project_id"`
	Minutes   int    `json:"minutes"`
}

// Meta files the submission.
func (s Submission) Meta() Meta { return Meta{RecSubmission, s.ID, s.Owner, s.EmployerID} }

// Final reports whether the submission can no longer change.
func (s Submission) Final() bool { return s.Status != SubmissionSubmitted }

// Validate checks the submission.
func (s Submission) Validate() error {
	if err := checkKeys(s.ID, s.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", s.EmployerID, true); err != nil {
		return err
	}
	if s.From.IsZero() || s.To.Before(s.From) {
		return invalid("submission needs from <= to")
	}
	switch s.Kind {
	case SubmitPeriod:
		if s.AbsenceID != "" || s.Supersedes != "" {
			return invalid("a period names no absence and supersedes nothing")
		}
	case SubmitLeave:
		if err := checkRef("absence", s.AbsenceID, true); err != nil {
			return err
		}
	case SubmitCorrection:
		if err := checkRef("supersedes", s.Supersedes, true); err != nil {
			return err
		}
		if strings.TrimSpace(s.Note) == "" {
			return invalid("a correction says why")
		}
	default:
		return invalid("submission kind %q", s.Kind)
	}
	if s.SubmittedAt.IsZero() {
		return invalid("submission without time")
	}
	switch s.Status {
	case SubmissionSubmitted:
		if s.DecidedAt != nil || s.DecidedBy != "" {
			return invalid("an open submission has no decision")
		}
	case SubmissionApproved, SubmissionRejected, SubmissionWithdrawn:
		if s.DecidedAt == nil || s.DecidedBy == "" {
			return invalid("a decision needs who and when")
		}
		if s.Status == SubmissionRejected && strings.TrimSpace(s.Reason) == "" {
			return invalid("a rejection needs a reason")
		}
	default:
		return invalid("submission status %q", s.Status)
	}
	if len(s.Note) > 2000 || len(s.Reason) > 2000 {
		return invalid("note or reason longer than 2000 characters")
	}
	if len(s.Proposal) > 0 && s.Kind != SubmitCorrection {
		return invalid("only a correction proposes values")
	}
	seen := map[Date]bool{}
	for _, p := range s.Proposal {
		switch {
		case !p.Date.Within(s.From, s.To) || seen[p.Date]:
			return invalid("a proposed day lies outside the period or twice in it")
		case p.Minutes < 0 || p.Minutes > 24*60:
			return invalid("proposed minutes out of range")
		case p.Absence != "" && ((p.Minutes != 0 && !p.HalfDay) || !CodedAbsence(p.Absence)):
			return invalid("a proposed absence has a short code, and minutes only next to half a day")
		case !p.Part.Valid() || (p.Part != "" && p.Minutes == 0):
			return invalid("a proposed half of a day needs minutes")
		case p.Absence == "" && p.HalfDay:
			return invalid("only an absence is half a day")
		case len(p.Note) > 2000:
			return invalid("a proposed note is longer than 2000 characters")
		case len(p.Typed) > 200 || (p.Typed != "" && p.Minutes == 0):
			return invalid("a proposed span needs minutes and is at most 200 characters")
		}
		projects := map[string]bool{}
		for _, x := range p.Projects {
			if err := checkRef("project", x.ProjectID, true); err != nil {
				return err
			}
			if projects[x.ProjectID] || x.Minutes < 0 || x.Minutes > 24*60 {
				return invalid("a proposed project lies twice in a day or its minutes are out of range")
			}
			projects[x.ProjectID] = true
		}
		seen[p.Date] = true
	}
	return nil
}

// PeriodAt returns the period submission that holds the day of the employer: handed in or approved, and not opened
// again by a correction.
func (c Catalog) PeriodAt(employerID string, d Date) (Submission, bool) {
	for _, s := range c.Submissions {
		if s.Kind == SubmitPeriod && s.EmployerID == employerID && d.Within(s.From, s.To) && c.binding(s) {
			return s, true
		}
	}
	return Submission{}, false
}

// OpenCorrection returns the correction that waits for a decision on the submission.
func (c Catalog) OpenCorrection(id string) (Submission, bool) {
	for _, s := range c.Submissions {
		if s.Kind == SubmitCorrection && s.Supersedes == id && s.Status == SubmissionSubmitted {
			return s, true
		}
	}
	return Submission{}, false
}

// reopened reports whether an approved correction opened the submission again.
func (c Catalog) reopened(id string) bool {
	for _, s := range c.Submissions {
		if s.Kind == SubmitCorrection && s.Supersedes == id && s.Status == SubmissionApproved {
			return true
		}
	}
	return false
}

// binding reports whether a submission holds its records: handed in or approved, and not opened again.
func (c Catalog) binding(s Submission) bool {
	return (s.Status == SubmissionSubmitted || s.Status == SubmissionApproved) && !c.reopened(s.ID)
}

// SubmittedLocked reports whether the day of the employer lies in a period that is handed in or approved.
func (c Catalog) SubmittedLocked(employerID string, d Date) bool {
	for _, s := range c.Submissions {
		if s.Kind == SubmitPeriod && (s.EmployerID == employerID || employerID == "") && d.Within(s.From, s.To) &&
			c.binding(s) {
			return true
		}
	}
	return false
}

// LeaveLocked reports whether a change of the absence old into next (nil for a deletion) is refused because of a leave
// request: while the request is open nothing changes, and after its approval only the decision itself (status
// requested to approved, with who approved) may still be written. A rejected or withdrawn request, and an approved one
// that a correction opened again, lock nothing.
func (c Catalog) LeaveLocked(old Absence, next *Absence) bool {
	for _, s := range c.Submissions {
		if s.Kind != SubmitLeave || s.AbsenceID != old.ID || s.Owner != old.Owner || !c.binding(s) {
			continue
		}
		if s.Status == SubmissionApproved && next != nil && old.Status == StatusRequested &&
			next.Status == StatusApproved && onlyStatusChanged(old, *next) {
			continue
		}
		return true
	}
	return false
}

func onlyStatusChanged(a, b Absence) bool {
	for _, x := range []*Absence{&a, &b} {
		x.Status, x.ApprovedBy, x.Version, x.Changed = "", "", 0, time.Time{}
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

// MayDecide reports whether the actor may decide submissions of the employer: an administrator of the organization
// that manages it, and never on their own.
func MayDecide(e Employer, a Actor, s Submission) bool {
	return e.Managed() && a.Admin && a.Organization != "" && a.Organization == e.Organization && a.Person != s.Owner
}

// SubmitRequest is what a person hands in.
type SubmitRequest struct {
	Kind       SubmissionKind
	EmployerID string
	From, To   Date
	AbsenceID  string
	Supersedes string
	Note       string
	// Proposal: the values a correction asks for (a change request); the days lie in the corrected period.
	Proposal []ProposedDay
	At       time.Time
}

// Submit makes a submission of the person's data, for an organization that has switched approval on. A period gets
// its target, actual and balance, and must not overlap another period that is handed in or approved; a leave request
// names a requested absence of the employer; a correction names an approved submission. The caller stores it.
func Submit(o Organization, d Data, r SubmitRequest) (Submission, error) {
	if !o.Features.Approval {
		return Submission{}, invalid("approval is not switched on")
	}
	emp, ok := d.Catalog.Employer(r.EmployerID)
	if !ok {
		return Submission{}, ErrNotFound
	}
	if !emp.Managed() || emp.Organization != o.ID {
		return Submission{}, invalid("only records of the organization are handed in to it")
	}
	at := r.At
	if at.IsZero() {
		at = time.Now()
	}
	s := Submission{ID: NewID(), Owner: emp.Owner, EmployerID: emp.ID, Kind: r.Kind, From: r.From, To: r.To,
		AbsenceID: r.AbsenceID, Supersedes: r.Supersedes, Status: SubmissionSubmitted, Note: r.Note,
		SubmittedAt: at.UTC(), Proposal: r.Proposal}
	switch r.Kind {
	case SubmitPeriod:
		for _, x := range d.Catalog.Submissions {
			if x.Kind == SubmitPeriod && x.EmployerID == emp.ID && d.Catalog.binding(x) &&
				!x.To.Before(r.From) && !x.From.After(r.To) {
				return Submission{}, invalid("the days overlap submission %s", x.ID)
			}
		}
		if r.To.Before(r.From) {
			return Submission{}, invalid("submission needs from <= to")
		}
		days, err := Days(d, emp.ID, r.From, r.To)
		if err != nil {
			return Submission{}, err
		}
		t := Sum(days)
		s.Target, s.Actual, s.Balance = t.AdjustedTarget, t.Actual, t.Balance
	case SubmitLeave:
		var found *Absence
		for i, a := range d.Absences {
			if a.ID == r.AbsenceID && a.DeletedAt == nil {
				found = &d.Absences[i]
			}
		}
		if found == nil || found.EmployerID != emp.ID {
			return Submission{}, invalid("no absence %q of the employer", r.AbsenceID)
		}
		if found.Status != StatusRequested {
			return Submission{}, invalid("the absence is not requested")
		}
		s.From, s.To = found.From, found.To
	case SubmitCorrection:
		var old *Submission
		for i, x := range d.Catalog.Submissions {
			if x.ID == r.Supersedes {
				old = &d.Catalog.Submissions[i]
			}
		}
		if old == nil || old.EmployerID != emp.ID || old.Status != SubmissionApproved || d.Catalog.reopened(old.ID) {
			return Submission{}, invalid("only an approved submission can be corrected")
		}
		s.From, s.To = old.From, old.To
	}
	return s, s.Validate()
}

// Decide approves or rejects a submitted submission. Rejecting needs a reason; the actor must be allowed to decide.
func Decide(s Submission, emp Employer, a Actor, approve bool, reason string, at time.Time) (Submission, error) {
	if s.Status != SubmissionSubmitted {
		return s, ErrLocked
	}
	if emp.ID != s.EmployerID || !MayDecide(emp, a, s) {
		return s, invalid("%s may not decide this submission", a.Person)
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	s.Status, s.DecidedAt, s.DecidedBy, s.Reason = SubmissionRejected, &at, a.Person, strings.TrimSpace(reason)
	if approve {
		s.Status = SubmissionApproved
	}
	return s, s.Validate()
}

// Withdraw takes back a submitted submission; only the person who handed it in can.
func Withdraw(s Submission, person string, at time.Time) (Submission, error) {
	if s.Status != SubmissionSubmitted {
		return s, ErrLocked
	}
	if person != s.Owner {
		return s, invalid("only the person withdraws")
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	s.Status, s.DecidedAt, s.DecidedBy = SubmissionWithdrawn, &at, person
	return s, s.Validate()
}

// LeaveDecision returns the absence of a decided leave request with the decision: approved or rejected, and who
// decided. Store the submission first, then the absence through UpdateAbsence.
func LeaveDecision(s Submission, a Absence) (Absence, error) {
	if s.Kind != SubmitLeave || s.AbsenceID != a.ID || (s.Status != SubmissionApproved && s.Status != SubmissionRejected) {
		return a, invalid("no decided leave request for absence %s", a.ID)
	}
	a.ApprovedBy = s.DecidedBy
	a.Status = StatusRejected
	if s.Status == SubmissionApproved {
		a.Status = StatusApproved
	}
	return a, nil
}

// ReviewRange returns the days a reviewer reads for the open (submitted) submissions of the catalog: from the first to
// the last day any of them covers, and today in any case. Read the organization's data for this range (ReadManaged)
// and pass it to ReviewData, so a leave request far ahead or a correction of an old week comes with its records,
// however far from today they lie.
func ReviewRange(c Catalog, today Date) (from, to Date) {
	from, to = today, today
	for _, s := range c.Submissions {
		if s.Status != SubmissionSubmitted || s.From.IsZero() || s.To.IsZero() {
			continue
		}
		if s.From.Before(from) {
			from = s.From
		}
		if s.To.After(to) {
			to = s.To
		}
	}
	return from, to
}

// ReviewData returns what a reviewer of the organization sees when approval is switched on: the entries of the days
// handed in or approved, and the absences inside them or asked for by a leave request that was not withdrawn. The
// rest of what Store.Managed returns stays with the payroll view. Without approval, d comes back unchanged.
func ReviewData(o Organization, d Data) Data {
	if !o.Features.Approval {
		return d
	}
	out := Data{Catalog: d.Catalog}
	shown := func(employerID string, day Date) bool {
		for _, s := range d.Catalog.Submissions {
			if s.Kind == SubmitPeriod && s.EmployerID == employerID && day.Within(s.From, s.To) &&
				s.Status != SubmissionWithdrawn {
				return true
			}
		}
		return false
	}
	asked := map[string]bool{}
	for _, s := range d.Catalog.Submissions {
		if s.Kind == SubmitLeave && s.Status != SubmissionWithdrawn {
			asked[s.Owner+"\x00"+s.AbsenceID] = true
		}
	}
	for _, e := range d.Entries {
		if shown(e.EmployerID, e.Date) {
			out.Entries = append(out.Entries, e)
		}
	}
	for _, a := range d.Absences {
		inside := true
		for day := a.From; !day.After(a.To); day = day.AddDays(1) {
			inside = inside && shown(a.EmployerID, day)
		}
		if inside || asked[a.Owner+"\x00"+a.ID] {
			out.Absences = append(out.Absences, a)
		}
	}
	return out
}
