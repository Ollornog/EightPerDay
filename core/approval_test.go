// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"slices"
	"strings"
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

// The notice applies to German employers and to those without a region; whoever stands for the employer records it.
func TestMayRecordLeaveNotice(t *testing.T) {
	private := employerOf(ModeSchedule, "DE-BY")
	managed := Employer{ID: "m", Owner: owner, Holder: HolderManaged, Organization: "o", Region: "DE"}
	admin, member := Actor{Person: "boss", Organization: "o", Admin: true}, Actor{Person: owner, Organization: "o"}
	cases := []struct {
		name string
		e    Employer
		a    Actor
		want bool
	}{
		{"the person on a private employer", private, Actor{Person: owner}, true},
		{"someone else on a private employer", private, Actor{Person: "boss"}, false},
		{"the administrator on a managed employer", managed, admin, true},
		{"the member on a managed employer", managed, member, false},
		{"an administrator of another organization", managed, Actor{Person: "boss", Organization: "x", Admin: true}, false},
		{"no region counts as German", employerOf(ModeSchedule, ""), Actor{Person: owner}, true},
		{"an Austrian employer", employerOf(ModeSchedule, "AT"), Actor{Person: owner}, false},
		{"a region that only starts with DE", employerOf(ModeSchedule, "DEX"), Actor{Person: owner}, false},
		{"a client, not an employment", Employer{ID: "c", Owner: owner, Kind: KindClient, Region: "DE"}, Actor{Person: owner}, false},
	}
	for _, c := range cases {
		if got := MayRecordLeaveNotice(c.e, c.a); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// An organization owes the notice to every German, active employer with open leave towards the end of the year and
// without a notice for that year.
func TestNoticesOwed(t *testing.T) {
	sched := func(employer string) Schedule {
		s := week("s"+employer, "2026-01-01", 480, 480, 480, 480, 480, 0, 0)
		s.EmployerID = employer
		return s
	}
	emp := func(id, region string, archived bool) Employer {
		return Employer{ID: id, Owner: owner, Holder: HolderManaged, Organization: "o", Mode: ModeSchedule, Region: region,
			WeekStart: time.Monday, Archived: archived}
	}
	data := Data{Catalog: Catalog{
		Employers: []Employer{emp("e3", "DE-BY", false), emp("e1", "", false), emp("e2", "DE", false), emp("at", "AT", false),
			emp("old", "DE", true)},
		Schedules: []Schedule{sched("e1"), sched("e2"), sched("e3"), sched("at"), sched("old")},
		LeaveNotices: []LeaveNotice{{ID: "n", Owner: owner, EmployerID: "e2", Year: 2026,
			NotifiedAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)}},
	}}
	got, err := NoticesOwed(data, d("2026-11-18"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].EmployerID != "e1" || got[1].EmployerID != "e3" || got[0].Remaining != 30 || got[0].NoticeGiven {
		t.Fatalf("%+v", got)
	}
	if early, err := NoticesOwed(data, d("2026-06-01"), 0); err != nil || len(early) != 0 {
		t.Fatalf("too early in the year: %+v %v", early, err)
	}
}

// A change request is a correction with the values it asks for: inside the approved period, one value per day,
// hours or an absence with a short code. Until it is decided the period stays binding.
func TestChangeRequest(t *testing.T) {
	data := approvalData()
	s, _ := Submit(approvalOn, data, period("2026-03-02", "2026-03-08"))
	approved, _ := Decide(s, acmeEmployer(), boss, true, "", decidedAt)
	data.Catalog.Submissions = []Submission{approved}
	if got, ok := data.Catalog.PeriodAt("e1", d("2026-03-04")); !ok || got.ID != approved.ID {
		t.Fatalf("PeriodAt %+v %v", got, ok)
	}
	if _, ok := data.Catalog.PeriodAt("e1", d("2026-03-09")); ok {
		t.Error("a day outside every period lies in one")
	}
	request := func(days ...ProposedDay) SubmitRequest {
		return SubmitRequest{Kind: SubmitCorrection, EmployerID: "e1", Supersedes: approved.ID, Note: "forgot the dentist",
			Proposal: days, At: decidedAt}
	}
	c, err := Submit(approvalOn, data, request(ProposedDay{Date: d("2026-03-03"), Minutes: 360, Typed: "8-14:30 30m",
		Note: strings.Repeat("dentist ", 100), Projects: []ProposedProject{{"p1", 120}, {"p2", 0}}},
		ProposedDay{Date: d("2026-03-04"), Absence: AbsenceVacation, HalfDay: true, Minutes: 240, Part: Afternoon,
			Details: &ProposedDetails{Place: "Berlin", KmTotal: 125, StandbyActive: 90, Callouts: 2},
			Incomes: []ProposedIncome{{Kind: IncomeTip, Amount: 500, Currency: "EUR", PaidBy: PaidCard}, {Remove: "i1"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if c.From != approved.From || len(c.Proposal) != 2 || c.Proposal[0].Minutes != 360 {
		t.Fatalf("change request %+v", c)
	}
	data.Catalog.Submissions = append(data.Catalog.Submissions, c)
	if open, ok := data.Catalog.OpenCorrection(approved.ID); !ok || open.ID != c.ID {
		t.Error("the open change request is not found")
	}
	if !data.Catalog.SubmittedLocked("e1", d("2026-03-03")) {
		t.Error("the approved values stop counting before the decision")
	}
	for name, days := range map[string][]ProposedDay{
		"outside the period":       {{Date: d("2026-03-09"), Minutes: 60}},
		"twice the same day":       {{Date: d("2026-03-03"), Minutes: 60}, {Date: d("2026-03-03"), Minutes: 120}},
		"more than a day":          {{Date: d("2026-03-03"), Minutes: 24*60 + 1}},
		"negative":                 {{Date: d("2026-03-03"), Minutes: -1}},
		"a type without a code":    {{Date: d("2026-03-03"), Absence: AbsenceUnpaid}},
		"absence with minutes":     {{Date: d("2026-03-03"), Absence: AbsenceVacation, Minutes: 60}},
		"half a day of hours":      {{Date: d("2026-03-03"), Minutes: 240, HalfDay: true}},
		"a part without hours":     {{Date: d("2026-03-03"), Part: Morning}},
		"an unknown part":          {{Date: d("2026-03-03"), Minutes: 240, Part: "noon"}},
		"a note over 2000 chars":   {{Date: d("2026-03-03"), Minutes: 60, Note: strings.Repeat("x", 2001)}},
		"a span without hours":     {{Date: d("2026-03-03"), Typed: "8-12"}},
		"a project twice":          {{Date: d("2026-03-03"), Projects: []ProposedProject{{"p1", 60}, {"p1", 120}}}},
		"a project without id":     {{Date: d("2026-03-03"), Projects: []ProposedProject{{"", 60}}}},
		"a project over a day":     {{Date: d("2026-03-03"), Projects: []ProposedProject{{"p1", 24*60 + 1}}}},
		"a negative project":       {{Date: d("2026-03-03"), Projects: []ProposedProject{{"p1", -1}}}},
		"a place over 200 chars":   {{Date: d("2026-03-03"), Details: &ProposedDetails{Place: strings.Repeat("x", 201)}}},
		"negative kilometres":      {{Date: d("2026-03-03"), Details: &ProposedDetails{KmOneWay: -1}}},
		"standby over a day":       {{Date: d("2026-03-03"), Details: &ProposedDetails{StandbyPassive: 1000, StandbyActive: 441}}},
		"too many call-outs":       {{Date: d("2026-03-03"), Details: &ProposedDetails{Callouts: 101}}},
		"an income without amount": {{Date: d("2026-03-03"), Incomes: []ProposedIncome{{Kind: IncomeTip, Currency: "EUR"}}}},
		"an income of no kind": {{Date: d("2026-03-03"), Incomes: []ProposedIncome{{Kind: "gift", Amount: 1,
			Currency: "EUR"}}}},
		"an income without currency": {{Date: d("2026-03-03"), Incomes: []ProposedIncome{{Kind: IncomeTip, Amount: 1}}}},
		"an income taken off twice":  {{Date: d("2026-03-03"), Incomes: []ProposedIncome{{Remove: "i1"}, {Remove: "i1"}}}},
		"an income taken off with an amount": {{Date: d("2026-03-03"), Incomes: []ProposedIncome{{Remove: "i1",
			Amount: 1}}}},
		"an income taken off by a bad key": {{Date: d("2026-03-03"), Incomes: []ProposedIncome{{Remove: "a b"}}}},
		"more than 50 incomes": {{Date: d("2026-03-03"), Incomes: slices.Repeat([]ProposedIncome{{Kind: IncomeTip,
			Amount: 1, Currency: "EUR"}}, 51)}},
	} {
		if _, err := Submit(approvalOn, data, request(days...)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	p := period("2026-03-09", "2026-03-15")
	p.Proposal = []ProposedDay{{Date: d("2026-03-09"), Minutes: 60}}
	if _, err := Submit(approvalOn, data, p); !errors.Is(err, ErrInvalid) {
		t.Errorf("a period with proposed values: %v", err)
	}
	// Approved, the change request opens the period again; the caller books the values and hands in the period anew.
	decided, err := Decide(c, acmeEmployer(), boss, true, "", decidedAt)
	if err != nil {
		t.Fatal(err)
	}
	data.Catalog.Submissions[1] = decided
	if _, ok := data.Catalog.PeriodAt("e1", d("2026-03-03")); ok {
		t.Error("the approved change request did not open the period")
	}
}
