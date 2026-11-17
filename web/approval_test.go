// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Ollornog/EightPerDay/core"
)

// approvalSite is an organization with approval switched on, its administrator (s) and a member (worker) with a
// managed employer in the mode plan.
func approvalSite(t *testing.T) (s, worker *site, org string, member *accountRef, managed core.Employer) {
	t.Helper()
	s, _ = trackSite(t)
	boss := s.me("boss", true)
	s.mustPost("/orgs", url.Values{"name": {"ACME"}, "region": {"DE-BY"}, "back": {"/orgs"}})
	memberships, _ := s.store.Memberships(context.Background(), "", boss.ID)
	org = memberships[0].Organization
	s.mustPost("/orgs/"+org+"/template", url.Values{"name": {"Full time"}, "mode": {"plan"}, "hours-1": {"8"}, "hours-2": {"8"},
		"hours-3": {"8"}, "hours-4": {"8"}, "hours-5": {"8"}, "leave_days": {"30"}, "basis": {"week"}, "back": {"/"}})
	s.mustPost("/orgs/"+org+"/features", url.Values{"approval": {"on"}, "confirm": {"on"}, "agreement": {"BV 2026"}, "back": {"/"}})
	worker = newSiteSharing(t, s)
	member = worker.me("tom", false)
	s.mustPost("/orgs/"+org+"/member", url.Values{"person": {member.ID}, "role": {"member"}, "template": {"Full time"},
		"from": {"2026-11-01"}, "records": {"on"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), member.ID)
	for _, e := range cat.Employers {
		if e.Managed() {
			managed = e
		}
	}
	return s, worker, org, member, managed
}

func submissions(t *testing.T, s *site, owner string) []core.Submission {
	t.Helper()
	cat, err := s.store.Catalog(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return cat.Submissions
}

func TestApprovalOfAWeek(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	home := "/?e=" + managed.ID
	if body := worker.get(home).body; !strings.Contains(body, "data-approval") || !strings.Contains(body, "Hand in this week") {
		t.Fatal("the week does not offer to be handed in")
	}
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {home}})
	subs := submissions(t, s, member.ID)
	if len(subs) != 1 || subs[0].Status != core.SubmissionSubmitted || subs[0].Target != 40*60 {
		t.Fatalf("%+v", subs)
	}
	// Handed in, the week is locked: the page says so, and a deviation is refused.
	body := worker.get(home).body
	if !strings.Contains(body, "data-week-locked") || !strings.Contains(body, "data-locked") || strings.Contains(body, "data-submit-week") {
		t.Fatal("the page does not show the locked week")
	}
	if a := worker.post("/book/adjust", url.Values{"employer": {managed.ID}, "date": {tuesday.String()}, "direction": {"less"},
		"minutes": {"1"}}); a.status != http.StatusBadRequest {
		t.Fatalf("a locked day took a deviation: %d", a.status)
	}
	// Only an administrator decides, and a rejection needs a reason.
	if a := worker.post("/orgs/"+org+"/decide", url.Values{"submission": {subs[0].ID}, "decision": {"approve"}}); a.status != http.StatusNotFound {
		t.Fatalf("a member decided: %d", a.status)
	}
	page := s.get("/orgs/" + org).body
	if !strings.Contains(page, `data-review="`+subs[0].ID+`"`) || !strings.Contains(page, "Mara tom") || !strings.Contains(page, "40:00") {
		t.Fatal("the administrator does not see the week handed in")
	}
	if a := s.post("/orgs/"+org+"/decide", url.Values{"submission": {subs[0].ID}, "decision": {"reject"}, "reason": {" "}}); a.status != http.StatusBadRequest ||
		!strings.Contains(a.body, "A rejection needs a reason.") {
		t.Fatalf("rejected without a reason: %d", a.status)
	}
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {subs[0].ID}, "decision": {"reject"}, "reason": {"Tuesday is missing"}, "back": {"/"}})
	if subs = submissions(t, s, member.ID); subs[0].Status != core.SubmissionRejected || subs[0].Reason != "Tuesday is missing" {
		t.Fatalf("%+v", subs)
	}
	// Rejected, the week is open again and the member reads why.
	body = worker.get(home).body
	if !strings.Contains(body, "Tuesday is missing") || !strings.Contains(body, "data-submit-week") {
		t.Fatal("the member does not see the rejection")
	}
	worker.mustPost("/book/adjust", url.Values{"employer": {managed.ID}, "date": {tuesday.String()}, "direction": {"less"}, "minutes": {"1"}, "back": {home}})

	// Handed in again and withdrawn; then handed in, approved, and opened again by an approved correction.
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {home}})
	open := only(t, submissions(t, s, member.ID), core.SubmissionSubmitted)
	worker.mustPost("/approval/"+open.ID+"/withdraw", url.Values{"back": {home}})
	if byID(submissions(t, s, member.ID), open.ID).Status != core.SubmissionWithdrawn {
		t.Fatal("not withdrawn")
	}
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {home}})
	week := only(t, submissions(t, s, member.ID), core.SubmissionSubmitted)
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {week.ID}, "decision": {"approve"}, "back": {"/"}})
	if a := worker.post("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"correction"}, "supersedes": {week.ID}}); a.status != http.StatusBadRequest {
		t.Fatalf("a correction without a reason: %d", a.status)
	}
	if body := worker.get(home).body; !strings.Contains(body, "data-correction") {
		t.Fatal("an approved week offers no correction")
	}
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"correction"}, "supersedes": {week.ID},
		"note": {"Friday was a day off"}, "back": {home}})
	correction := only(t, submissions(t, s, member.ID), core.SubmissionSubmitted)
	if correction.Kind != core.SubmitCorrection || correction.Supersedes != week.ID {
		t.Fatalf("%+v", correction)
	}
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {correction.ID}, "decision": {"approve"}, "back": {"/"}})
	if body := worker.get(home).body; strings.Contains(body, "data-week-locked") || strings.Contains(body, "data-correction") {
		t.Fatal("the approved correction did not open the week")
	}

	// Every decision and every look of the administrator is in the member's access log, in a chain that holds.
	log := worker.get("/access").body
	for _, want := range []string{"Decided", "Read", "Mara boss", "ACME", "approval", "The log is complete"} {
		if !strings.Contains(log, want) {
			t.Errorf("the access log lacks %q", want)
		}
	}
	if own := s.get("/access").body; !strings.Contains(own, "data-access-none") {
		t.Error("the administrator's own log lists something")
	}
}

// only returns the one submission in the status.
func only(t *testing.T, subs []core.Submission, status core.SubmissionStatus) core.Submission {
	t.Helper()
	var found []core.Submission
	for _, s := range subs {
		if s.Status == status {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d submissions %s: %+v", len(found), status, subs)
	}
	return found[0]
}

func byID(subs []core.Submission, id string) core.Submission {
	for _, s := range subs {
		if s.ID == id {
			return s
		}
	}
	return core.Submission{}
}

func TestApprovalOfLeave(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	home := "/?e=" + managed.ID
	// Leave is requested and handed in at once; sickness needs no approval.
	worker.mustPost("/book/absence", url.Values{"employer": {managed.ID}, "type": {"vacation"}, "from": {"2026-11-26"}, "to": {"2026-11-27"}, "back": {home}})
	worker.mustPost("/book/absence", url.Values{"employer": {managed.ID}, "type": {"sick"}, "from": {monday.String()}, "back": {home}})
	absences, _ := s.store.Absences(context.Background(), core.AbsenceFilter{Owner: member.ID})
	var leave core.Absence
	for _, a := range absences {
		switch a.Type {
		case "vacation":
			leave = a
			if a.Status != core.StatusRequested {
				t.Errorf("leave is %s", a.Status)
			}
		case "sick":
			if a.Status != core.StatusApproved {
				t.Errorf("sickness is %s", a.Status)
			}
		}
	}
	subs := submissions(t, s, member.ID)
	if len(subs) != 1 || subs[0].Kind != core.SubmitLeave || subs[0].AbsenceID != leave.ID {
		t.Fatalf("%+v", subs)
	}
	if page := s.get("/orgs/" + org).body; !strings.Contains(page, "Leave request: Leave") {
		t.Fatal("the administrator does not see the leave request")
	}
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {subs[0].ID}, "decision": {"approve"}, "back": {"/"}})
	absences, _ = s.store.Absences(context.Background(), core.AbsenceFilter{Owner: member.ID})
	for _, a := range absences {
		if a.ID == leave.ID && a.Status != core.StatusApproved {
			t.Fatalf("the approved leave is %s", a.Status)
		}
	}
	if body := worker.get("/?e=" + managed.ID + "&w=2026-11-23").body; strings.Contains(body, "waits for approval") || !strings.Contains(body, "approved") {
		t.Fatal("the member does not see the approval")
	}
}

// Without the feature, or on a private employer, nothing is handed in.
func TestNoApprovalWithoutTheFeature(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Mine", core.ModeSchedule, "8")
	if body := s.get("/?e=" + emp.ID).body; strings.Contains(body, "data-approval") {
		t.Fatal("a private employer offers approval")
	}
	if a := s.post("/approval/submit", url.Values{"employer": {emp.ID}, "kind": {"period"}, "from": {monday.String()}, "to": {monday.String()}}); a.status != http.StatusBadRequest {
		t.Fatalf("handed in without the feature: %d", a.status)
	}
	s.mustPost("/book/absence", url.Values{"employer": {emp.ID}, "type": {"vacation"}, "from": {monday.String()}, "back": {"/"}})
	if absences, _ := s.store.Absences(context.Background(), core.AbsenceFilter{Owner: me.ID}); len(absences) != 1 || absences[0].Status != core.StatusApproved {
		t.Fatalf("%+v", absences)
	}
}

// An administrator reading or changing a member's settings shows in the member's access log.
func TestSettingsAccessIsLogged(t *testing.T) {
	s, worker, _, member, managed := approvalSite(t)
	s.get("/employers/" + managed.ID + "?owner=" + member.ID)
	s.mustPost("/employers/"+managed.ID+"/pay", url.Values{"owner": {member.ID}, "basis": {"hourly"}, "amount": {"21"}, "from": {"2026-01-01"}, "back": {"/"}})
	log := worker.get("/access").body
	if !strings.Contains(log, "employer settings: pay") || !strings.Contains(log, `data-access="read"`) {
		t.Fatalf("the settings access is not logged: %.2000s", log)
	}
	// The member's own reading of the settings is not logged.
	before := strings.Count(log, "data-access=")
	worker.get("/employers/" + managed.ID)
	if after := strings.Count(worker.get("/access").body, "data-access="); after != before {
		t.Fatalf("own reads logged: %d to %d", before, after)
	}
}

func TestPaidAbsencesAndSurcharges(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Hospital", core.ModeExact, "8")
	settings := "/employers/" + emp.ID
	s.mustPost(settings+"/pay", url.Values{"basis": {"hourly"}, "amount": {"20"}, "from": {"2026-01-01"}, "back": {settings}})
	s.mustPost(settings+"/absence-pay", url.Values{"type": {"vacation"}, "method": {"target"}, "from": {"2026-01-01"}, "back": {settings}})
	s.mustPost(settings+"/surcharge", url.Values{"kind": {"night"}, "percent": {"25"}, "window_start": {"22:00"}, "window_end": {"6:00"},
		"from": {"2026-01-01"}, "back": {settings}})
	s.mustPost(settings+"/surcharge", url.Values{"kind": {"sunday"}, "percent": {"12,5"}, "from": {"2026-01-01"}, "back": {settings}})
	for _, bad := range []url.Values{
		{"kind": {"night"}, "percent": {"25"}, "from": {"2026-01-01"}},                      // a night needs its window
		{"kind": {"sunday"}, "percent": {"0"}, "from": {"2026-01-01"}},                      // nothing on top
		{"kind": {"sunday"}, "percent": {"lots"}, "from": {"2026-01-01"}},                   // not a number
		{"kind": {"sunday"}, "percent": {"10"}, "window_start": {"1"}, "window_end": {"2"}}, // no date
	} {
		if a := s.post(settings+"/surcharge", bad); a.status != http.StatusBadRequest {
			t.Errorf("%v: %d", bad, a.status)
		}
	}
	if a := s.post(settings+"/absence-pay", url.Values{"type": {"vacation"}, "method": {"sometimes"}, "from": {"2026-01-01"}}); a.status != http.StatusBadRequest {
		t.Errorf("a made-up method: %d", a.status)
	}
	page := s.get(settings).body
	for _, want := range []string{`Night <span class="tabular-nums">25 %</span>`, "22:00–6:00", `Sunday <span class="tabular-nums">12.5 %</span>`,
		"Leave · At the planned hours"} {
		if !strings.Contains(page, want) {
			t.Errorf("the settings lack %q", want)
		}
	}
	// A night span from 20:00 to 23:00 on Monday, leave on Tuesday.
	s.mustPost("/book/span", url.Values{"employer": {emp.ID}, "date": {monday.String()}, "start": {"20:00"}, "end": {"23:00"}, "back": {"/"}})
	s.mustPost("/book/absence", url.Values{"employer": {emp.ID}, "type": {"vacation"}, "from": {tuesday.String()}, "back": {"/"}})
	report := s.get("/reports?month=2026-11").body
	for _, want := range []string{"Paid absence: Leave", "Surcharge: Night 25 %", "160.00 EUR", "5.00 EUR"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q", want)
		}
	}
	// The day split of night shifts is a setting of the employer.
	s.mustPost(settings+"/details", url.Values{"name": {"Hospital"}, "kind": {"employer"}, "day_split": {"midnight"}, "back": {settings}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if e, _ := cat.Employer(emp.ID); e.DaySplit != core.SplitMidnight {
		t.Fatalf("day split %q", e.DaySplit)
	}
	if a := s.post(settings+"/details", url.Values{"name": {"Hospital"}, "kind": {"employer"}, "day_split": {"noon"}}); a.status != http.StatusBadRequest {
		t.Fatalf("a made-up day split: %d", a.status)
	}
	// Removing a surcharge.
	for _, sc := range cat.Surcharges {
		s.mustPost(settings+"/delete", url.Values{"kind": {"surcharge"}, "record": {sc.ID}, "back": {settings}})
	}
	if cat, _ := s.store.Catalog(context.Background(), me.ID); len(cat.Surcharges) != 0 {
		t.Fatal("the surcharges stay")
	}
}

func TestFormatPercent(t *testing.T) {
	for _, c := range []struct {
		lang string
		bp   int
		want string
	}{{"en", 2500, "25 %"}, {"de", 1250, "12,5 %"}, {"en", 1205, "12.05 %"}, {"en", 5, "0.05 %"}} {
		if got := formatPercent(c.lang, c.bp); got != c.want {
			t.Errorf("%d %s: %q", c.bp, c.lang, got)
		}
	}
}
