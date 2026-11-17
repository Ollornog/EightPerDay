// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"testing"
	"time"
)

var (
	approvalOn = Organization{ID: "acme", Name: "ACME", Features: CompanyFeatures{Approval: true, Agreement: "BV 2026-01",
		ConfirmedBy: "boss", ConfirmedAt: func() *time.Time { t := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); return &t }()}}
	boss      = Actor{Person: "boss", Organization: "acme", Admin: true}
	decidedAt = time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
)

func acmeEmployer() Employer {
	e := employerOf(ModeSchedule, "DE-BY")
	e.Holder, e.Organization = HolderManaged, "acme"
	return e
}

func approvalData() Data {
	return Data{Catalog: Catalog{Employers: []Employer{acmeEmployer()},
		Schedules: []Schedule{week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0)}},
		Entries: []Entry{work("2026-03-02", 540), work("2026-03-03", 480)}}
}

func period(from, to string) SubmitRequest {
	return SubmitRequest{Kind: SubmitPeriod, EmployerID: "e1", From: d(from), To: d(to), At: decidedAt}
}

func TestSubmit(t *testing.T) {
	data := approvalData()
	s, err := Submit(approvalOn, data, period("2026-03-02", "2026-03-08"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != SubmissionSubmitted || s.Owner != owner || s.Target != 5*480 || s.Actual != 1020 || s.Balance != 1020-5*480 {
		t.Fatalf("period: %+v", s)
	}

	off := approvalOn
	off.Features = CompanyFeatures{}
	private := approvalData()
	private.Catalog.Employers = []Employer{employerOf(ModeSchedule, "DE-BY")}
	other := approvalOn
	other.ID = "globex"
	for name, c := range map[string]struct {
		o Organization
		d Data
		r SubmitRequest
	}{
		"approval switched off":     {off, data, period("2026-03-02", "2026-03-08")},
		"a private employer":        {approvalOn, private, period("2026-03-02", "2026-03-08")},
		"another organization":      {other, data, period("2026-03-02", "2026-03-08")},
		"backwards":                 {approvalOn, data, period("2026-03-08", "2026-03-02")},
		"leave without the absence": {approvalOn, data, SubmitRequest{Kind: SubmitLeave, EmployerID: "e1", AbsenceID: "nope"}},
		"correction of nothing":     {approvalOn, data, SubmitRequest{Kind: SubmitCorrection, EmployerID: "e1", Supersedes: "nope", Note: "x"}},
		"an unknown kind":           {approvalOn, data, SubmitRequest{Kind: "wish", EmployerID: "e1", From: d("2026-03-02"), To: d("2026-03-02")}},
	} {
		if _, err := Submit(c.o, c.d, c.r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Submit(approvalOn, data, SubmitRequest{Kind: SubmitPeriod, EmployerID: "nobody"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown employer: %v", err)
	}

	// Overlap: refused while the first is open or approved, allowed once it was withdrawn, rejected or reopened.
	data.Catalog.Submissions = []Submission{s}
	if _, err := Submit(approvalOn, data, period("2026-03-08", "2026-03-15")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("overlap accepted: %v", err)
	}
	if _, err := Submit(approvalOn, data, period("2026-03-09", "2026-03-15")); err != nil {
		t.Fatalf("next week: %v", err)
	}
	approved, err := Decide(s, acmeEmployer(), boss, true, "", decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	data.Catalog.Submissions = []Submission{approved}
	if _, err := Submit(approvalOn, data, period("2026-03-02", "2026-03-02")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("overlap with an approved period accepted: %v", err)
	}
	fix, err := Submit(approvalOn, data, SubmitRequest{Kind: SubmitCorrection, EmployerID: "e1", Supersedes: s.ID,
		Note: "forgot the doctor on Tuesday", At: decidedAt})
	if err != nil || fix.From != s.From || fix.To != s.To {
		t.Fatalf("correction: %+v %v", fix, err)
	}
	if _, err := Submit(approvalOn, data, SubmitRequest{Kind: SubmitCorrection, EmployerID: "e1", Supersedes: s.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("correction without a reason: %v", err)
	}
	fixed, err := Decide(fix, acmeEmployer(), boss, true, "", decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	data.Catalog.Submissions = []Submission{approved, fixed}
	if _, err := Submit(approvalOn, data, period("2026-03-02", "2026-03-08")); err != nil {
		t.Fatalf("handing in again after the correction: %v", err)
	}
	if _, err := Submit(approvalOn, data, SubmitRequest{Kind: SubmitCorrection, EmployerID: "e1", Supersedes: s.ID, Note: "again"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a reopened submission corrected twice: %v", err)
	}
}

func TestDecideAndWithdraw(t *testing.T) {
	s, err := Submit(approvalOn, approvalData(), period("2026-03-02", "2026-03-08"))
	if err != nil {
		t.Fatal(err)
	}
	emp := acmeEmployer()
	cases := []struct {
		name    string
		actor   Actor
		approve bool
		reason  string
		ok      bool
	}{
		{"an administrator approves", boss, true, "", true},
		{"an administrator rejects with a reason", boss, false, "Tuesday is missing", true},
		{"a rejection needs a reason", boss, false, "  ", false},
		{"a member may not", Actor{Person: "carol", Organization: "acme"}, true, "", false},
		{"an administrator of another organization may not", Actor{Person: "x", Organization: "globex", Admin: true}, true, "", false},
		{"nobody approves their own", Actor{Person: owner, Organization: "acme", Admin: true}, true, "", false},
	}
	for _, c := range cases {
		got, err := Decide(s, emp, c.actor, c.approve, c.reason, decidedAt)
		if c.ok != (err == nil) {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if c.ok && (got.DecidedBy != c.actor.Person || !got.DecidedAt.Equal(decidedAt) || got.Final() == false ||
			(got.Status == SubmissionApproved) != c.approve) {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
	approved, _ := Decide(s, emp, boss, true, "", decidedAt)
	if _, err := Decide(approved, emp, boss, false, "changed my mind", decidedAt); !errors.Is(err, ErrLocked) {
		t.Fatalf("decided twice: %v", err)
	}
	if _, err := Withdraw(approved, owner, decidedAt); !errors.Is(err, ErrLocked) {
		t.Fatalf("withdrawn after approval: %v", err)
	}
	if _, err := Withdraw(s, "boss", decidedAt); !errors.Is(err, ErrInvalid) {
		t.Fatalf("withdrawn by somebody else: %v", err)
	}
	w, err := Withdraw(s, owner, decidedAt)
	if err != nil || w.Status != SubmissionWithdrawn || !w.Final() {
		t.Fatalf("withdraw: %+v %v", w, err)
	}
}

func TestSubmissionValidation(t *testing.T) {
	good := Submission{ID: "s", Owner: owner, EmployerID: "e1", Kind: SubmitPeriod, From: d("2026-03-02"),
		To: d("2026-03-08"), Status: SubmissionSubmitted, SubmittedAt: decidedAt}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	at := decidedAt
	for name, change := range map[string]func(*Submission){
		"no time":                     func(s *Submission) { s.SubmittedAt = time.Time{} },
		"backwards":                   func(s *Submission) { s.To = d("2026-03-01") },
		"period with an absence":      func(s *Submission) { s.AbsenceID = "a" },
		"leave without absence":       func(s *Submission) { s.Kind = SubmitLeave },
		"correction without a reason": func(s *Submission) { s.Kind, s.Supersedes = SubmitCorrection, "x" },
		"open with a decision":        func(s *Submission) { s.DecidedAt, s.DecidedBy = &at, "boss" },
		"approved without who":        func(s *Submission) { s.Status, s.DecidedAt = SubmissionApproved, &at },
		"rejected without a reason":   func(s *Submission) { s.Status, s.DecidedAt, s.DecidedBy = SubmissionRejected, &at, "boss" },
		"unknown status":              func(s *Submission) { s.Status = "maybe" },
	} {
		s := good
		change(&s)
		if err := s.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSubmissionLocks(t *testing.T) {
	open := Submission{ID: "p", Owner: owner, EmployerID: "e1", Kind: SubmitPeriod, From: d("2026-03-02"),
		To: d("2026-03-08"), Status: SubmissionSubmitted}
	with := func(status SubmissionStatus) Submission { s := open; s.Status = status; return s }
	reopen := Submission{ID: "c", Owner: owner, EmployerID: "e1", Kind: SubmitCorrection, Supersedes: "p",
		From: open.From, To: open.To, Status: SubmissionApproved}
	pendingFix := reopen
	pendingFix.Status = SubmissionSubmitted
	cases := []struct {
		name   string
		subs   []Submission
		locked bool
	}{
		{"handed in", []Submission{open}, true},
		{"approved", []Submission{with(SubmissionApproved)}, true},
		{"rejected", []Submission{with(SubmissionRejected)}, false},
		{"withdrawn", []Submission{with(SubmissionWithdrawn)}, false},
		{"approved and opened again by a correction", []Submission{with(SubmissionApproved), reopen}, false},
		{"a correction that waits opens nothing", []Submission{with(SubmissionApproved), pendingFix}, true},
	}
	for _, c := range cases {
		cat := Catalog{Employers: []Employer{acmeEmployer()}, Submissions: c.subs}
		if got := cat.Locked("e1", d("2026-03-04")); got != c.locked {
			t.Errorf("%s: locked %v, want %v", c.name, got, c.locked)
		}
		if cat.Locked("e1", d("2026-03-09")) || cat.Locked("e2", d("2026-03-04")) {
			t.Errorf("%s: locks beyond its days or employer", c.name)
		}
		in := Absence{ID: "a", Owner: owner, EmployerID: "e1", From: d("2026-03-08"), To: d("2026-03-10")}
		if got := cat.AbsenceLocked(in); got != c.locked {
			t.Errorf("%s: absence locked %v, want %v", c.name, got, c.locked)
		}
	}

	// Leave: open requests lock; after the approval only the decision itself may be written.
	requested := Absence{ID: "a", Owner: owner, EmployerID: "e1", Type: AbsenceVacation, From: d("2026-08-03"),
		To: d("2026-08-07"), Status: StatusRequested, Version: 1}
	leave := Submission{ID: "l", Owner: owner, EmployerID: "e1", Kind: SubmitLeave, AbsenceID: "a",
		From: requested.From, To: requested.To, Status: SubmissionSubmitted}
	decided := leave
	decided.Status, decided.DecidedBy = SubmissionApproved, "boss"
	approvedAbsence, err := LeaveDecision(decided, requested)
	if err != nil || approvedAbsence.Status != StatusApproved || approvedAbsence.ApprovedBy != "boss" {
		t.Fatalf("leave decision: %+v %v", approvedAbsence, err)
	}
	approvedAbsence.Version = 2
	longer := approvedAbsence
	longer.To = d("2026-08-14")
	sneaky := requested
	sneaky.To, sneaky.Status = d("2026-08-14"), StatusApproved
	cancelLeave := Submission{ID: "c", Owner: owner, EmployerID: "e1", Kind: SubmitCorrection, Supersedes: "l",
		Status: SubmissionApproved}
	for _, c := range []struct {
		name      string
		subs      []Submission
		old, next Absence
		deleted   bool
		locked    bool
	}{
		{"open request: no change", []Submission{leave}, requested, sneaky, false, true},
		{"open request: no deletion", []Submission{leave}, requested, Absence{}, true, true},
		{"approval writes the decision", []Submission{decided}, requested, approvedAbsence, false, false},
		{"approval does not let the days change", []Submission{decided}, requested, sneaky, false, true},
		{"approved: no change", []Submission{decided}, approvedAbsence, longer, false, true},
		{"approved: no deletion", []Submission{decided}, approvedAbsence, Absence{}, true, true},
		{"corrected: change", []Submission{decided, cancelLeave}, approvedAbsence, longer, false, false},
		{"corrected: deletion", []Submission{decided, cancelLeave}, approvedAbsence, Absence{}, true, false},
		{"another absence", []Submission{decided}, Absence{ID: "b", Owner: owner}, Absence{ID: "b", Owner: owner}, false, false},
	} {
		cat := Catalog{Submissions: c.subs}
		next := &c.next
		if c.deleted {
			next = nil
		}
		if got := cat.LeaveLocked(c.old, next); got != c.locked {
			t.Errorf("%s: locked %v, want %v", c.name, got, c.locked)
		}
	}
	if _, err := LeaveDecision(leave, requested); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an open request decided the absence: %v", err)
	}
}

func TestReviewData(t *testing.T) {
	data := approvalData()
	data.Entries = append(data.Entries, work("2026-03-10", 480))
	asked := absence(AbsenceVacation, "2026-08-03", "2026-08-07")
	asked.Status = StatusRequested
	data.Absences = []Absence{absence(AbsenceSick, "2026-03-04", "2026-03-04"), asked,
		absence(AbsenceSick, "2026-03-11", "2026-03-11")}
	data.Catalog.Submissions = []Submission{
		{ID: "p", Owner: owner, EmployerID: "e1", Kind: SubmitPeriod, From: d("2026-03-02"), To: d("2026-03-08"),
			Status: SubmissionApproved},
		{ID: "w", Owner: owner, EmployerID: "e1", Kind: SubmitPeriod, From: d("2026-03-09"), To: d("2026-03-15"),
			Status: SubmissionWithdrawn},
		{ID: "l", Owner: owner, EmployerID: "e1", Kind: SubmitLeave, AbsenceID: asked.ID, From: asked.From, To: asked.To,
			Status: SubmissionSubmitted},
	}
	got := ReviewData(approvalOn, data)
	if len(got.Entries) != 2 || got.Entries[0].Date != d("2026-03-02") || got.Entries[1].Date != d("2026-03-03") {
		t.Fatalf("entries for review: %+v", got.Entries)
	}
	if len(got.Absences) != 2 || got.Absences[0].Type != AbsenceSick || got.Absences[1].ID != asked.ID {
		t.Fatalf("absences for review: %+v", got.Absences)
	}
	off := approvalOn
	off.Features = CompanyFeatures{}
	if all := ReviewData(off, data); len(all.Entries) != 3 || len(all.Absences) != 3 {
		t.Fatalf("without approval everything stays: %+v", all)
	}
}

func TestLeaveReminders(t *testing.T) {
	sched := week("s", "2026-01-01", 480, 480, 480, 480, 480, 0, 0) // 30 days of leave
	taken := absence(AbsenceVacation, "2026-08-03", "2026-08-28")   // 20 working days
	notice := LeaveNotice{ID: "n", Owner: owner, EmployerID: "e1", Year: 2026,
		NotifiedAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC), Text: "Please take your remaining leave."}
	cases := []struct {
		name     string
		today    string
		warn     int
		absences []Absence
		notices  []LeaveNotice
		want     *LeaveReminder
	}{
		{"too early", "2026-09-29", 0, []Absence{taken}, nil, nil},
		{"open leave without notice", "2026-10-01", 0, []Absence{taken}, nil,
			&LeaveReminder{Remaining: 10, DaysLeft: 91, Lapses: false}},
		{"open leave with notice", "2026-12-01", 0, []Absence{taken}, []LeaveNotice{notice},
			&LeaveReminder{Remaining: 10, DaysLeft: 30, NoticeGiven: true, Lapses: true}},
		{"a notice of another year does not count", "2026-12-01", 0, []Absence{taken},
			[]LeaveNotice{{ID: "n", Owner: owner, EmployerID: "e1", Year: 2025, NotifiedAt: time.Now()}},
			&LeaveReminder{Remaining: 10, DaysLeft: 30}},
		{"own lead time", "2026-11-01", 30, []Absence{taken}, nil, nil},
		{"nothing left", "2026-12-01", 0, []Absence{taken, absence(AbsenceVacation, "2026-11-02", "2026-11-13")}, nil, nil},
	}
	for _, c := range cases {
		data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "")}, Schedules: []Schedule{sched},
			LeaveNotices: c.notices}, Absences: c.absences}
		got, err := LeaveReminders(data, "e1", d(c.today), c.warn)
		if err != nil {
			t.Fatal(err)
		}
		if c.want == nil {
			if len(got) != 0 {
				t.Errorf("%s: %+v", c.name, got)
			}
			continue
		}
		if len(got) != 1 {
			t.Errorf("%s: %d reminders", c.name, len(got))
			continue
		}
		r := got[0]
		if r.Remaining != c.want.Remaining || r.DaysLeft != c.want.DaysLeft || r.NoticeGiven != c.want.NoticeGiven ||
			r.Lapses != c.want.Lapses || r.Expires != d("2026-12-31") || r.Year != 2026 || (r.NotifiedAt != nil) != r.NoticeGiven {
			t.Errorf("%s: %+v", c.name, r)
		}
	}
}
