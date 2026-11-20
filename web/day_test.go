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

// A craftsman's day both ways: hours typed in the list hold the bookings on clients and the rest is not booked on
// any; a day without typed hours is made by its bookings. The weekly report prints the clients with their numbers.
func TestProjectsOfADayAndTheWeeklyReport(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Workshop", core.ModeSchedule, "8")
	s.mustCell(monday, "e-"+emp.ID, "8")
	day := "/day?e=" + emp.ID + "&d=2026-11-16"
	book := func(date, project, number, hours string) {
		s.mustPost("/book/project-day", url.Values{"employer": {emp.ID}, "back": {day}, "date": {date}, "project": {project},
			"number": {number}, "hours": {hours}})
	}
	book("2026-11-16", "Miller", "A-17", "3")
	book("2026-11-16", "miller", "a-17", "1,5") // the same client and order, written another way
	book("2026-11-16", "Miller", "A-18", "2")
	book("2026-11-17", "Baker", "", "6")
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if len(cat.Projects) != 3 {
		t.Fatalf("projects %+v", cat.Projects)
	}
	body := s.get(day).body
	if attr(body, "data-typed") != "8" || attr(body, "data-worked") != "8" || attr(body, "data-day-unbooked") != "1.5" ||
		strings.Count(body, "data-booking") != 3 {
		t.Errorf("Monday: typed %s worked %s unbooked %s", attr(body, "data-typed"), attr(body, "data-worked"), attr(body, "data-day-unbooked"))
	}
	if body := s.get("/day?e=" + emp.ID + "&d=2026-11-17").body; attr(body, "data-worked") != "6" || attr(body, "data-typed") != "–" {
		t.Errorf("Tuesday is made by its booking: %s", attr(body, "data-worked"))
	}
	body = s.get("/").body
	week := body[strings.Index(body, `data-week="2026-11-16"`):]
	if attr(week, "data-open") != "1.5 open" || attr(week, "data-week-sum") != "14" {
		t.Errorf("sheet: %s / %s", attr(week, "data-open"), attr(week, "data-week-sum"))
	}
	if fieldValue(t, body, tuesday, "e-"+emp.ID) != "" || !strings.Contains(body, `placeholder="6"`) {
		t.Error("Tuesday's field stays empty and shows the booked hours")
	}
	report := s.get("/report/week?e=" + emp.ID + "&w=2026-11-16").body
	for _, want := range []string{"Weekly report, week 47", "Miller · A-17 · 4.5", "Miller · A-18 · 2", "Baker · 6",
		"data-report-sum>14<", "data-report-diff>-10<", "data-print", "print-sheet"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q", want)
		}
	}
	if strings.Contains(report, "tab-bar") {
		t.Error("the report to print shows the tab bar")
	}
	if a := s.post("/book/project-day", url.Values{"employer": {emp.ID}, "back": {day}, "date": {"2026-11-16"}, "project": {""}, "hours": {"1"}}); a.status != 400 {
		t.Errorf("a booking without a project: %d", a.status)
	}
	if a := s.post("/book/project-day", url.Values{"employer": {emp.ID}, "back": {day}, "date": {"2026-11-16"}, "project": {"X"}, "hours": {"0"}}); a.status != 400 {
		t.Errorf("a booking without hours: %d", a.status)
	}
}

// The details of a day and its incomes are fixed with the week (T-18): handed in, every way refuses them; approved,
// saving the details, adding an income and taking one off become a change request with a reason. The day's page says
// so and shows what is asked; the reviewer reads it, the approval books it and the access log shows it.
func TestDayDetailsAndIncomesOfAnApprovedWeek(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	ctx := context.Background()
	page := "/day?e=" + managed.ID + "&d=" + monday.String()
	form := func(pairs ...string) url.Values {
		f := url.Values{"employer": {managed.ID}, "date": {monday.String()}, "back": {page}}
		for i := 0; i+1 < len(pairs); i += 2 {
			f.Set(pairs[i], pairs[i+1])
		}
		return f
	}
	catalog := func() core.Catalog {
		cat, err := s.store.Catalog(ctx, member.ID)
		if err != nil {
			t.Fatal(err)
		}
		return cat
	}
	worker.mustCell(monday, "e-"+managed.ID, "8")
	worker.mustPost("/book/dayinfo", form("place", "Berlin", "standby_active", "1", "callouts", "1"))
	worker.mustPost("/book/income", form("kind", "tip", "amount", "5", "paid_by", "cash"))
	tip := catalog().Incomes[0]
	unchanged := func(when string) {
		t.Helper()
		cat := catalog()
		info, _ := cat.DayInfoOf(managed.ID, monday)
		if info.Place != "Berlin" || info.StandbyActive != 60 || info.Callouts != 1 || len(cat.Incomes) != 1 || cat.Incomes[0].ID != tip.ID {
			t.Fatalf("%s: the details or incomes changed: %+v %+v", when, info, cat.Incomes)
		}
	}
	ways := map[string]url.Values{
		"/book/dayinfo":                  form("place", "Hamburg", "standby_active", "2", "callouts", "2"),
		"/book/income":                   form("kind", "job", "client", "Miller", "amount", "100", "paid_by", "card"),
		"/incomes/" + tip.ID + "/remove": form(),
	}
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {"/"}})
	for path, f := range ways {
		f.Set("reason", "why")
		if a := worker.post(path, f); a.status != http.StatusBadRequest || !strings.Contains(a.body, "cannot be changed") {
			t.Errorf("handed in, %s: %d", path, a.status)
		}
	}
	unchanged("handed in")
	if body := worker.get(page).body; !strings.Contains(body, "data-day-fixed") || strings.Contains(body, "data-income-form") ||
		strings.Contains(body, "data-income-remove") || !strings.Contains(body, "<fieldset class=\"grid gap-2\" disabled>") {
		t.Error("the day of a week handed in offers to change its details or incomes")
	}

	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {submissions(t, s, member.ID)[0].ID}, "decision": {"approve"}, "back": {"/"}})
	if body := worker.get(page).body; !strings.Contains(body, "data-day-request") || !strings.Contains(body, `id="di-reason" name="reason"`) ||
		!strings.Contains(body, `id="in-reason" name="reason"`) || !strings.Contains(body, `id="ir-`+tip.ID+`" name="reason"`) {
		t.Error("the day of an approved week does not ask for a reason")
	}
	for path, f := range ways {
		f.Del("reason")
		if a := worker.post(path, f); a.status != http.StatusBadRequest || !strings.Contains(a.body, "says why") {
			t.Errorf("approved, %s without a reason: %d", path, a.status)
		}
		f.Set("reason", "the call-out at night")
		if a := worker.mustPost(path, f); !strings.Contains(a.header.Get("Location"), "done=requested") {
			t.Errorf("approved, %s: %s", path, a.header.Get("Location"))
		}
	}
	unchanged("asked for")
	// The same income taken off again asks for nothing new; the same details saved again are no change at all.
	if a := worker.mustPost("/incomes/"+tip.ID+"/remove", form("reason", "again")); !strings.Contains(a.header.Get("Location"), "done=requested") {
		t.Errorf("taken off again: %s", a.header.Get("Location"))
	}
	if a := worker.mustPost("/book/dayinfo", form("place", "Berlin", "standby_active", "1", "callouts", "1")); !strings.Contains(a.header.Get("Location"), "done=saved") {
		t.Errorf("the same details: %s", a.header.Get("Location"))
	}
	var request core.Submission
	for _, x := range submissions(t, s, member.ID) {
		if x.Kind == core.SubmitCorrection {
			request = x
		}
	}
	if p := request.Proposal; len(p) != 1 || p[0].Minutes != 480 || p[0].Details == nil ||
		*p[0].Details != (core.ProposedDetails{Place: "Hamburg", StandbyActive: 120, Callouts: 2}) || len(p[0].Incomes) != 2 {
		t.Fatalf("change request %+v", request.Proposal)
	}
	want := "place Hamburg, called out 2 h, 2 call-outs"
	if body := worker.get(page).body; !strings.Contains(body, "data-day-pending") || !strings.Contains(body, want) {
		t.Error("the day's page does not show what is asked")
	}
	review := s.get("/orgs/" + org).body
	for _, text := range []string{want, "+ Job, Miller 100.00 EUR, Card", "− Tip 5.00 EUR, Cash"} {
		if !strings.Contains(review, text) {
			t.Errorf("the reviewer does not read %q", text)
		}
	}
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {request.ID}, "decision": {"approve"}, "back": {"/"}})
	cat := catalog()
	info, _ := cat.DayInfoOf(managed.ID, monday)
	if info.Place != "Hamburg" || info.StandbyActive != 120 || info.Callouts != 2 || len(cat.Incomes) != 1 ||
		cat.Incomes[0].Client != "Miller" || cat.Incomes[0].Amount != 10000 || cat.Incomes[0].PaidBy != core.PaidCard {
		t.Fatalf("the approved request was not booked: %+v %+v", info, cat.Incomes)
	}
	if log := worker.get("/access").body; !strings.Contains(log, "Changed") {
		t.Error("the access log lacks the change")
	}
	// Approved again, the day is fixed again.
	if a := worker.post("/book/dayinfo", form("place", "Rome")); a.status != http.StatusBadRequest {
		t.Errorf("the week approved again took details in place: %d", a.status)
	}
}

// A change request that takes off an income of another day, or another employer's, is not approved halfway: the
// decision is refused before anything is stored.
func TestChangeRequestTakesOffOnlyItsDaysIncomes(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	ctx := context.Background()
	worker.mustCell(monday, "e-"+managed.ID, "8")
	other := core.Income{ID: core.NewID(), Owner: member.ID, EmployerID: managed.ID, Date: monday.AddDays(14), Kind: core.IncomeTip,
		Amount: 500, Currency: "EUR"}
	if err := s.store.Put(ctx, other); err != nil {
		t.Fatal(err)
	}
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {"/"}})
	period := submissions(t, s, member.ID)[0]
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {period.ID}, "decision": {"approve"}, "back": {"/"}})
	cat, _ := s.store.Catalog(ctx, member.ID)
	orgRecord, _ := s.store.Organization(ctx, org)
	c, err := core.Submit(orgRecord, core.Data{Catalog: cat}, core.SubmitRequest{Kind: core.SubmitCorrection, EmployerID: managed.ID,
		Supersedes: period.ID, Note: "forged", At: testNow,
		Proposal: []core.ProposedDay{{Date: monday, Minutes: 480, Incomes: []core.ProposedIncome{{Remove: other.ID}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.Put(ctx, c); err != nil {
		t.Fatal(err)
	}
	if a := s.post("/orgs/"+org+"/decide", url.Values{"submission": {c.ID}, "decision": {"approve"}, "back": {"/"}}); a.status == http.StatusSeeOther {
		t.Error("the change request was approved")
	}
	cat, _ = s.store.Catalog(ctx, member.ID)
	if open, ok := cat.OpenCorrection(period.ID); !ok || open.ID != c.ID || len(cat.Incomes) != 1 {
		t.Errorf("the refused decision changed something: %+v %+v", cat.Submissions, cat.Incomes)
	}
}
