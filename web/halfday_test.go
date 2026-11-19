// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Ollornog/EightPerDay/core"
)

// A ski instructor paid per day, with an own amount for half a day: a whole day, a morning, an afternoon and half a
// day of leave with an afternoon of work, typed into the list. The rows show what was typed, the entries keep the
// half, and the week of every employer pays and averages it.
func TestHalfDaysInTheList(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	ski := s.employer(me, "Ski school", core.ModeSchedule, "")
	s.mustPost("/employers/"+ski.ID+"/pay", url.Values{"basis": {"per_assignment"}, "unit": {"day"}, "amount": {"120"},
		"half_amount": {"70"}, "from": {"2026-01-01"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if len(cat.PayRules) != 1 || cat.PayRules[0].HalfAmount != 7000 {
		t.Fatalf("pay rules %+v", cat.PayRules)
	}
	if page := s.get("/employers/" + ski.ID).body; !strings.Contains(page, "data-half-amount>half a day 70.00 EUR<") {
		t.Error("the settings do not show the amount for half a day")
	}
	back := "/?e=" + ski.ID + "&w=2026-11-16"
	s.mustPost("/book/list", url.Values{"employer": {ski.ID}, "back": {back},
		"v-2026-11-16": {"6"}, "was-2026-11-16": {""},
		"v-2026-11-17": {"3v"}, "was-2026-11-17": {""},
		"v-2026-11-18": {"3 PM"}, "was-2026-11-18": {""},
		"v-2026-11-19": {"½V 3p"}, "was-2026-11-19": {""}})
	body := s.get(back).body
	for date, want := range map[string]string{"2026-11-16": "6", "2026-11-17": "3 AM", "2026-11-18": "3 PM", "2026-11-19": "½V 3 PM"} {
		if got := rowValue(t, body, core.MustDate(date)); got != want {
			t.Errorf("%s shows %q, want %q", date, got, want)
		}
	}
	parts := map[string]core.DayPart{}
	for _, e := range s.entries(me.ID) {
		parts[e.Date.String()] = e.Part
	}
	if parts["2026-11-16"] != "" || parts["2026-11-17"] != core.Morning || parts["2026-11-18"] != core.Afternoon ||
		parts["2026-11-19"] != core.Afternoon {
		t.Errorf("parts %v", parts)
	}
	if a := s.absences(me.ID); len(a) != 1 || !a[0].HalfDay || a[0].From != core.MustDate("2026-11-19") {
		t.Errorf("absences %+v", a)
	}
	// Saved again unchanged, nothing is booked twice; the morning typed whole makes the day whole.
	s.mustPost("/book/list", url.Values{"employer": {ski.ID}, "back": {back},
		"v-2026-11-17": {"3"}, "was-2026-11-17": {"3 AM"}, "v-2026-11-19": {"½V 3 PM"}, "was-2026-11-19": {"½V 3 PM"}})
	if e := s.entries(me.ID); len(e) != 4 {
		t.Fatalf("%d entries", len(e))
	}
	if got := rowValue(t, s.get(back).body, core.MustDate("2026-11-17")); got != "3" {
		t.Errorf("the whole day shows %q", got)
	}
	s.mustPost("/book/list", url.Values{"employer": {ski.ID}, "back": {back}, "v-2026-11-17": {"3v"}, "was-2026-11-17": {"3"}})

	all := s.get("/all?w=2026-11-16").body
	tuesday := all[strings.Index(all, `data-all-day="2026-11-17"`):strings.Index(all, `data-all-day="2026-11-18"`)]
	if attr(tuesday, "data-hours") != "3 AM" || attr(tuesday, "data-day-earned") != "70.00 EUR" {
		t.Errorf("Tuesday: %s / %s", attr(tuesday, "data-hours"), attr(tuesday, "data-day-earned"))
	}
	// 120 + 3 × 70 in five halves of a day: 330 / 2,5 = 132.
	for name, want := range map[string]string{"data-week-earned": "330.00 EUR", "data-average": "132.00 EUR"} {
		if got := attr(all, name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if !strings.Contains(all, `data-work-halves="5"`) {
		t.Error("the week does not count its halves")
	}
	// The report names the half days.
	if report := s.get("/reports?month=2026-11").body; !strings.Contains(report, "Per assignment, half days") {
		t.Error("the report does not name the half days")
	}
}

// A taxi with standby: passive per standby day, active per hour, and whether standby counts as working time. The pay
// goes into the day and the week; the time account takes active standby by default, none when switched off.
func TestStandbyPayAndWorkingTime(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	taxi := s.employer(me, "Taxi", core.ModeSchedule, "")
	s.mustPost("/employers/"+taxi.ID+"/pay", url.Values{"basis": {"standby_passive"}, "unit": {"day"}, "amount": {"25"},
		"from": {"2026-01-01"}, "back": {"/"}})
	s.mustPost("/employers/"+taxi.ID+"/pay", url.Values{"basis": {"standby_active"}, "amount": {"30"}, "from": {"2026-01-01"},
		"back": {"/"}})
	if a := s.post("/employers/"+taxi.ID+"/pay", url.Values{"basis": {"standby_passive"}, "unit": {"assignment"},
		"amount": {"30"}, "from": {"2026-01-01"}, "back": {"/"}}); a.status != 400 {
		t.Errorf("passive standby per call-out: %d", a.status)
	}
	s.mustPost("/book/dayinfo", url.Values{"employer": {taxi.ID}, "back": {"/"}, "date": {"2026-11-17"},
		"standby_passive": {"8"}, "standby_active": {"1,5"}, "callouts": {"2"}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if len(cat.DayInfos) != 1 || cat.DayInfos[0].Callouts != 2 || cat.DayInfos[0].StandbyActive != 90 {
		t.Fatalf("day details %+v", cat.DayInfos)
	}
	day := s.get("/day?e=" + taxi.ID + "&d=2026-11-17").body
	if !strings.Contains(day, `value="2"`) || attr(day, "data-standby-work") != "1.5 hours of standby count as working time." {
		t.Errorf("the day: %s", attr(day, "data-standby-work"))
	}
	all := s.get("/all?w=2026-11-16").body
	tuesday := all[strings.Index(all, `data-all-day="2026-11-17"`):strings.Index(all, `data-all-day="2026-11-18"`)]
	// 25 for the standby day and 1,5 hours called out at 30.
	if attr(tuesday, "data-day-earned") != "70.00 EUR" || attr(all, "data-week-earned") != "70.00 EUR" ||
		!strings.Contains(tuesday, "data-standby") || attr(tuesday, "data-hours") != "1.5" {
		t.Errorf("Tuesday: %s, %s", attr(tuesday, "data-day-earned"), attr(tuesday, "data-hours"))
	}
	if report := s.get("/reports?month=2026-11").body; !strings.Contains(report, "Standby, passive / day") ||
		!strings.Contains(report, "Standby, active / hour") {
		t.Error("the report does not name the standby")
	}

	// Switched off, standby is no working time; the pay stays.
	s.mustPost("/employers/"+taxi.ID+"/details", url.Values{"name": {"Taxi"}, "kind": {"employer"}, "region": {"DE-BY"},
		"standby_work": {"none"}, "back": {"/"}})
	cat, _ = s.store.Catalog(context.Background(), me.ID)
	if emp, _ := cat.Employer(taxi.ID); emp.StandbyWork != core.StandbyWorkNone {
		t.Fatalf("standby work %q", emp.StandbyWork)
	}
	if strings.Contains(s.get("/day?e="+taxi.ID+"&d=2026-11-17").body, "data-standby-work") {
		t.Error("standby still counts as working time")
	}
	all = s.get("/all?w=2026-11-16").body
	if attr(all, "data-week-earned") != "70.00 EUR" {
		t.Errorf("the week earns %s", attr(all, "data-week-earned"))
	}
	// Back to the default, the setting is stored empty.
	s.mustPost("/employers/"+taxi.ID+"/details", url.Values{"name": {"Taxi"}, "kind": {"employer"}, "region": {"DE-BY"},
		"standby_work": {"active"}, "back": {"/"}})
	cat, _ = s.store.Catalog(context.Background(), me.ID)
	if emp, _ := cat.Employer(taxi.ID); emp.StandbyWork != "" {
		t.Errorf("standby work %q", emp.StandbyWork)
	}
	if a := s.post("/book/dayinfo", url.Values{"employer": {taxi.ID}, "back": {"/"}, "date": {"2026-11-17"}, "callouts": {"many"}}); a.status != 400 {
		t.Errorf("call-outs as words: %d", a.status)
	}
}

// Half a day of leave with an afternoon of work, asked for on an approved week: the request keeps both, the reviewer
// reads them, and approved, both are booked.
func TestChangeRequestWithHalfLeaveAndHours(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	list := "/?e=" + managed.ID
	worker.mustPost("/book/list", url.Values{"employer": {managed.ID}, "back": {list}, "v-2026-11-16": {"8"}, "was-2026-11-16": {""}})
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {list}})
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {submissions(t, s, member.ID)[0].ID}, "decision": {"approve"}, "back": {"/"}})
	worker.mustPost("/book/list", url.Values{"employer": {managed.ID}, "back": {list}, "v-2026-11-16": {"½V 4p"}, "was-2026-11-16": {"8"},
		"reason": {"left at noon"}})
	var request core.Submission
	for _, x := range submissions(t, s, member.ID) {
		if x.Kind == core.SubmitCorrection {
			request = x
		}
	}
	if len(request.Proposal) != 1 || request.Proposal[0].Minutes != 240 || request.Proposal[0].Part != core.Afternoon ||
		request.Proposal[0].Absence != core.AbsenceVacation || !request.Proposal[0].HalfDay {
		t.Fatalf("change request %+v", request.Proposal)
	}
	if body := worker.get(list).body; !strings.Contains(body, "data-proposed>→ ½V 4 PM<") {
		t.Error("the list does not show the request")
	}
	if page := s.get("/orgs/" + org).body; !strings.Contains(page, "Mon 11-16: ½V 4 PM") {
		t.Error("the reviewer does not see what is asked")
	}
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {request.ID}, "decision": {"approve"}, "back": {"/"}})
	if e := worker.entries(member.ID); len(e) != 1 || e[0].Minutes != 240 || e[0].Part != core.Afternoon {
		t.Fatalf("entries %+v", e)
	}
	if a := worker.absences(member.ID); len(a) != 1 || !a[0].HalfDay || a[0].Status != core.StatusApproved {
		t.Fatalf("absences %+v", a)
	}
}
