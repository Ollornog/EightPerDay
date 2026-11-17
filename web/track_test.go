// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

// clock is the time of a test site; it moves only when a test moves it.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func trackSite(t *testing.T) (*site, *clock) {
	t.Helper()
	c := &clock{now: testNow}
	return newSite(t, func(o *Options) { o.Now = c.Now }), c
}

var (
	monday    = core.MustDate("2026-11-16")
	tuesday   = core.MustDate("2026-11-17")
	wednesday = core.MustDate("2026-11-18")
)

// employer adds a private employer through the start form and returns it.
func (s *site) employer(owner *accountRef, name string, mode core.Mode, weekdayHours string) core.Employer {
	s.t.Helper()
	a := s.post("/employers", url.Values{"name": {name}, "kind": {"employer"}, "mode": {string(mode)}, "weekday_hours": {weekdayHours},
		"leave_days": {"30"}, "region": {"DE-BY"}})
	if a.status != http.StatusSeeOther {
		s.t.Fatalf("new employer: %d %.300s", a.status, a.body)
	}
	cat, err := s.store.Catalog(context.Background(), owner.ID)
	if err != nil {
		s.t.Fatal(err)
	}
	for _, e := range cat.Employers {
		if e.Name == name {
			return e
		}
	}
	s.t.Fatal("employer not stored")
	return core.Employer{}
}

type accountRef struct{ ID string }

func (s *site) me(name string, admin bool) *accountRef {
	return &accountRef{ID: s.account(name, admin).ID}
}

func (s *site) entries(owner string) []core.Entry {
	s.t.Helper()
	entries, err := s.store.Entries(context.Background(), core.EntryFilter{Owner: owner})
	if err != nil {
		s.t.Fatal(err)
	}
	var live []core.Entry
	for _, e := range entries {
		if e.DeletedAt == nil {
			live = append(live, e)
		}
	}
	return live
}

func (s *site) mustPost(path string, form url.Values) answer {
	s.t.Helper()
	a := s.post(path, form)
	if a.status != http.StatusSeeOther {
		s.t.Fatalf("%s %v: %d %.400s", path, form, a.status, a.body)
	}
	return a
}

func TestStartPageAsksForAnEmployer(t *testing.T) {
	s, _ := trackSite(t)
	s.me("mara", false)
	body := s.get("/").body
	for _, want := range []string{"Who do you work for?", `value="week_sum"`, `value="plan" checked`, "Plan counts", "Projects only"} {
		if !strings.Contains(body, want) {
			t.Errorf("the start page lacks %q", want)
		}
	}
}

func TestWeeklyTotal(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Workshop", core.ModeWeekSum, "")
	s.mustPost("/book/week", url.Values{"employer": {emp.ID}, "week": {wednesday.String()}, "hours": {"38,5"}, "back": {"/"}})
	entries := s.entries(me.ID)
	if len(entries) != 1 || entries[0].Minutes != 38*60+30 || entries[0].Precision != core.PrecisionWeek || entries[0].Date != monday {
		t.Fatalf("%+v", entries)
	}
	if body := s.get("/?e=" + emp.ID).body; !strings.Contains(body, `value="38:30"`) {
		t.Fatal("the week does not show its total")
	}
	// The next week starts empty.
	if body := s.get("/?e=" + emp.ID + "&w=2026-11-23").body; strings.Contains(body, `value="38:30"`) {
		t.Fatal("the next week is not empty")
	}
	// Changed, then cleared: one entry is updated, then removed, never a second one.
	s.mustPost("/book/week", url.Values{"employer": {emp.ID}, "week": {monday.String()}, "hours": {"40h"}, "back": {"/"}})
	if entries := s.entries(me.ID); len(entries) != 1 || entries[0].Minutes != 40*60 || entries[0].Version != 2 {
		t.Fatalf("%+v", entries)
	}
	s.mustPost("/book/week", url.Values{"employer": {emp.ID}, "week": {monday.String()}, "hours": {""}, "back": {"/"}})
	if entries := s.entries(me.ID); len(entries) != 0 {
		t.Fatalf("%+v", entries)
	}
	if a := s.post("/book/week", url.Values{"employer": {emp.ID}, "week": {monday.String()}, "hours": {"lots"}}); a.status != http.StatusBadRequest {
		t.Fatalf("%d", a.status)
	}
}

func TestScheduleWithBalance(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	s.mustPost("/book/days", url.Values{"employer": {emp.ID}, "back": {"/"}, "day-2026-11-16": {"8:30"}, "day-2026-11-17": {"7"}})
	body := s.get("/?e=" + emp.ID).body
	balances := regexp.MustCompile(`data-balance>([^<]*)<`).FindAllStringSubmatch(body, -1)
	if len(balances) < 2 || balances[0][1] != "+0:30" || balances[1][1] != "-1:00" {
		t.Fatalf("balances %v", balances)
	}
	// Up to today: +0:30 and -1:00, and today, Wednesday, has nothing booked yet.
	if !strings.Contains(body, `data-week-balance>-8:30<`) {
		t.Errorf("the week balance is not -8:30: %s", regexp.MustCompile(`data-week-balance>[^<]*`).FindString(body))
	}
	// 30 days a year, prorated from the schedule's start on 1 November.
	if !strings.Contains(body, `data-leave>5 / 5<`) {
		t.Errorf("the leave account: %s", regexp.MustCompile(`data-leave>[^<]*`).FindString(body))
	}
}

func TestPlanCountsOnlyDeviations(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Site", core.ModePlan, "8")
	body := s.get("/?e=" + emp.ID).body
	if !strings.Contains(body, "as planned") || !strings.Contains(body, "The week is right") {
		t.Fatal("the plan view is missing")
	}
	// Tuesday two hours earlier, Monday sick.
	s.mustPost("/book/adjust", url.Values{"employer": {emp.ID}, "date": {tuesday.String()}, "direction": {"less"}, "minutes": {"2"}, "note": {"dentist"}, "back": {"/"}})
	s.mustPost("/book/absence", url.Values{"employer": {emp.ID}, "type": {"sick"}, "from": {monday.String()}, "back": {"/"}})
	entries := s.entries(me.ID)
	if len(entries) != 1 || entries[0].Kind != core.EntryAdjust || entries[0].Minutes != -120 {
		t.Fatalf("%+v", entries)
	}
	s.mustPost("/book/confirm", url.Values{"employer": {emp.ID}, "week": {monday.String()}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if len(cat.Confirmations) != 1 || cat.Confirmations[0].From != monday || cat.Confirmations[0].To != wednesday {
		t.Fatalf("%+v", cat.Confirmations)
	}
	body = s.get("/?e=" + emp.ID).body
	if !strings.Contains(body, "You confirmed this week") || !strings.Contains(body, ">Sick<") {
		t.Fatal("confirmation or sickness does not show")
	}
	// Monday sick reduces the target, Tuesday is two hours short: the week so far is -2:00.
	if !strings.Contains(body, `data-week-balance>-2:00<`) {
		t.Errorf("week balance: %s", regexp.MustCompile(`data-week-balance>[^<]*`).FindString(body))
	}
}

func TestPlanBecomesExact(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Site", core.ModePlan, "8")
	s.mustPost("/book/exact", url.Values{"employer": {emp.ID}, "week": {monday.String()}, "back": {"/"}})
	entries := s.entries(me.ID)
	if len(entries) != 3 { // Monday to Wednesday, the days up to today
		t.Fatalf("%d entries: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Minutes != 480 {
			t.Errorf("%+v", e)
		}
	}
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if core.ModeAt(cat, emp.ID, monday) != core.ModeExact {
		t.Fatal("the mode did not switch")
	}
	if body := s.get("/?e=" + emp.ID).body; !strings.Contains(body, "Stopwatch") {
		t.Fatal("the exact view does not show")
	}
}

func TestFreeTextLine(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeExact, "8")
	s.mustPost("/book/line", url.Values{"employer": {emp.ID}, "line": {"8-16:30 30m Pause @ACME/Website #meeting"}, "back": {"/"}})
	entries := s.entries(me.ID)
	if len(entries) != 2 {
		t.Fatalf("%+v", entries)
	}
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if len(cat.Projects) != 1 || cat.Projects[0].Name != "Website" || entries[0].ProjectID != cat.Projects[0].ID && entries[1].ProjectID != cat.Projects[0].ID {
		t.Fatalf("the project was not resolved: %+v %+v", cat.Projects, entries)
	}
	s.mustPost("/book/line", url.Values{"employer": {emp.ID}, "line": {"Mo-Di Urlaub"}, "back": {"/"}})
	absences, _ := s.store.Absences(context.Background(), core.AbsenceFilter{Owner: me.ID})
	if len(absences) != 1 || absences[0].Type != "vacation" || absences[0].From != monday || absences[0].To != tuesday {
		t.Fatalf("%+v", absences)
	}
	if a := s.post("/book/line", url.Values{"employer": {emp.ID}, "line": {"@a @b"}}); a.status != http.StatusBadRequest || !strings.Contains(a.body, "Not understood") {
		t.Fatalf("%d %.300s", a.status, a.body)
	}
	preview := s.get("/api/parse?line=" + url.QueryEscape("Fr -2h früher weg"))
	if !strings.Contains(preview.body, "Deviation -2:00") || !strings.Contains(preview.body, "Fri") {
		t.Fatalf("%s", preview.body)
	}
}

func TestStopwatch(t *testing.T) {
	s, c := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeExact, "")
	s.mustPost("/employers/"+emp.ID+"/project", url.Values{"name": {"Roof"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	project := cat.Projects[0].ID
	s.mustPost("/timer/start", url.Values{"employer": {emp.ID}, "project": {project}, "note": {"tiles"}, "back": {"/"}})
	if a := s.post("/timer/start", url.Values{"employer": {emp.ID}, "back": {"/"}}); a.status != http.StatusBadRequest {
		t.Fatalf("a second stopwatch: %d", a.status)
	}
	if body := s.get("/?e=" + emp.ID).body; !strings.Contains(body, "Running since 10:30") {
		t.Fatalf("the running stopwatch does not show: %s", regexp.MustCompile(`Running since[^<]*`).FindString(body))
	}
	c.Add(95 * time.Minute)
	s.mustPost("/timer/stop", url.Values{"back": {"/"}})
	entries := s.entries(me.ID)
	if len(entries) != 1 || entries[0].Minutes != 95 || entries[0].Source != core.SourceTimer || entries[0].ProjectID != project ||
		entries[0].Note != "tiles" || entries[0].Precision != core.PrecisionSpan {
		t.Fatalf("%+v", entries)
	}
	if running, _ := s.auth.RunningTimer(context.Background(), me.ID); running != nil {
		t.Fatal("the stopwatch still runs")
	}
}

func TestSpanAndRemoval(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeExact, "")
	s.mustPost("/book/span", url.Values{"employer": {emp.ID}, "date": {tuesday.String()}, "start": {"22:00"}, "end": {"02:00"}, "break": {"15m"}, "back": {"/"}})
	entries := s.entries(me.ID)
	if len(entries) != 2 {
		t.Fatalf("%+v", entries)
	}
	var work core.Entry
	for _, e := range entries {
		if e.Kind == core.EntryWork {
			work = e
		}
	}
	if work.Minutes != 240 || work.Date != tuesday {
		t.Fatalf("a night shift past midnight: %+v", work)
	}
	s.mustPost("/entries/"+work.ID+"/remove", url.Values{"version": {"1"}, "reason": {"wrong day"}, "back": {"/"}})
	if len(s.entries(me.ID)) != 1 {
		t.Fatal("not removed")
	}
	history, _ := s.store.Revisions(context.Background(), me.ID, work.ID)
	if len(history) != 2 || history[1].Reason != "wrong day" {
		t.Fatalf("the history does not keep the removal with its reason: %+v", history)
	}
	if a := s.post("/book/span", url.Values{"employer": {emp.ID}, "date": {tuesday.String()}, "start": {"x"}, "end": {"9"}}); a.status != http.StatusBadRequest {
		t.Fatal(a.status)
	}
}

func TestProjectsOnly(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Studio", core.ModeProjectsOnly, "")
	s.mustPost("/employers/"+emp.ID+"/project", url.Values{"name": {"Logo"}, "back": {"/"}})
	s.mustPost("/employers/"+emp.ID+"/project", url.Values{"name": {"Website"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	form := url.Values{"employer": {emp.ID}, "week": {monday.String()}, "back": {"/"}}
	for _, p := range cat.Projects {
		form.Set("project-"+p.ID, map[string]string{"Logo": "3:15", "Website": "6"}[p.Name])
	}
	s.mustPost("/book/projects", form)
	total := 0
	for _, e := range s.entries(me.ID) {
		total += e.Minutes
	}
	if total != 3*60+15+6*60 {
		t.Fatalf("%d", total)
	}
	body := s.get("/?e=" + emp.ID).body
	if !strings.Contains(body, `value="3:15"`) || strings.Contains(body, "data-week-balance") {
		t.Fatal("projects only shows its values and no balance")
	}
}

func TestSettingsAndEarnings(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Client A", core.ModeWeekSum, "")
	s.mustPost("/employers/"+emp.ID+"/pay", url.Values{"basis": {"hourly"}, "amount": {"25,50"}, "currency": {"eur"}, "from": {"2026-01-01"}, "back": {"/"}})
	s.mustPost("/employers/"+emp.ID+"/mode", url.Values{"mode": {"exact"}, "from": {"2026-11-23"}, "back": {"/"}})
	s.mustPost("/employers/"+emp.ID+"/schedule", url.Values{"from": {"2026-11-01"}, "hours-1": {"8"}, "hours-5": {"6"}, "start-1": {"8:00"},
		"end-1": {"16:30"}, "leave_days": {"28,5"}, "basis": {"month"}, "back": {"/"}})
	s.mustPost("/book/week", url.Values{"employer": {emp.ID}, "week": {monday.String()}, "hours": {"10"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if len(cat.PayRules) != 1 || cat.PayRules[0].Amount != 2550 || cat.PayRules[0].Currency != "EUR" {
		t.Fatalf("%+v", cat.PayRules)
	}
	if core.ModeAt(cat, emp.ID, monday) != core.ModeWeekSum || core.ModeAt(cat, emp.ID, core.MustDate("2026-11-23")) != core.ModeExact {
		t.Fatal("the mode change does not start on its date")
	}
	if len(cat.Schedules) != 1 || cat.Schedules[0].Days[time.Monday].Minutes != 480 || cat.Schedules[0].LeaveDays != 28.5 ||
		cat.Schedules[0].Days[time.Monday].Start == nil {
		t.Fatalf("%+v", cat.Schedules)
	}
	page := s.get("/employers/" + emp.ID).body
	if !strings.Contains(page, "25.50 EUR") || !strings.Contains(page, `data-current-mode="week_sum"`) {
		t.Fatalf("the settings do not show the pay or the mode: %s", regexp.MustCompile(`data-payrule[\s\S]{0,300}|data-current-mode="[^"]*"`).FindAllString(page, -1))
	}
	report := s.get("/reports?month=2026-11").body
	if !strings.Contains(report, "255.00 EUR") {
		t.Fatalf("ten hours at 25.50 are not 255.00: %s", regexp.MustCompile(`data-earnings>[\s\S]{0,120}`).FindString(report))
	}
	// A new rate from November on does not change the hours booked before it: they keep their rate.
	s.mustPost("/employers/"+emp.ID+"/pay", url.Values{"basis": {"hourly"}, "amount": {"30"}, "from": {"2026-11-01"}, "back": {"/"}})
	if report := s.get("/reports?month=2026-11").body; !strings.Contains(report, "255.00 EUR") {
		t.Fatalf("a later rate re-valued booked hours: %s", regexp.MustCompile(`data-earnings>[\s\S]{0,120}`).FindString(report))
	}
	s.post("/language", url.Values{"lang": {"de"}, "back": {"/"}})
	if report := s.get("/reports?month=2026-11").body; !strings.Contains(report, "255,00 EUR") || !strings.Contains(report, "November 2026") {
		t.Fatal("the German report does not write money the German way")
	}
	if a := s.post("/employers/"+emp.ID+"/pay", url.Values{"basis": {"hourly"}, "amount": {"-3"}, "from": {"2026-01-01"}}); a.status != http.StatusBadRequest {
		t.Fatalf("a negative amount: %d", a.status)
	}
}

func TestExport(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeWeekSum, "")
	s.mustPost("/book/week", url.Values{"employer": {emp.ID}, "week": {monday.String()}, "hours": {"12"}, "back": {"/"}})
	a := s.get("/export/json")
	if a.status != http.StatusOK || !strings.Contains(a.header.Get("Content-Disposition"), "attachment") || !strings.Contains(a.body, `"minutes": 720`) {
		t.Fatalf("%d %s %.300s", a.status, a.header.Get("Content-Disposition"), a.body)
	}
	if a := s.get("/export/csv"); a.status != http.StatusOK || !strings.Contains(a.header.Get("Content-Type"), "text/csv") || !strings.Contains(a.body, "720") {
		t.Fatalf("%d %.300s", a.status, a.body)
	}
}

// Nobody books on, reads or changes another person's private employer: it does not exist for them.
func TestPrivateEmployersStayPrivate(t *testing.T) {
	s, _ := trackSite(t)
	owner := s.me("mara", false)
	emp := s.employer(owner, "Mine", core.ModeWeekSum, "")
	other := newSiteSharing(t, s)
	other.me("eve", false)
	for _, c := range []struct {
		path string
		form url.Values
	}{
		{"/book/week", url.Values{"employer": {emp.ID}, "week": {monday.String()}, "hours": {"5"}}},
		{"/book/line", url.Values{"employer": {emp.ID}, "line": {"2h"}}},
		{"/employers/" + emp.ID + "/pay", url.Values{"owner": {owner.ID}, "basis": {"hourly"}, "amount": {"1"}, "from": {"2026-01-01"}}},
	} {
		if a := other.post(c.path, c.form); a.status == http.StatusSeeOther {
			t.Errorf("%s: another person changed it", c.path)
		}
	}
	if a := other.get("/employers/" + emp.ID + "?owner=" + owner.ID); a.status != http.StatusNotFound {
		t.Errorf("another person reads the settings: %d", a.status)
	}
	if len(s.entries(owner.ID)) != 0 {
		t.Fatal("something was booked on the private employer")
	}
}

// newSiteSharing is a second browser on the same program.
func newSiteSharing(t *testing.T, s *site) *site {
	other := *s
	other.client = s.newClient()
	return &other
}

func TestOrganizations(t *testing.T) {
	s, _ := trackSite(t)
	boss := s.me("boss", true)
	// Founding needs an administrator of the program.
	s.mustPost("/orgs", url.Values{"name": {"ACME Construction"}, "region": {"DE-BY"}, "back": {"/orgs"}})
	memberships, _ := s.store.Memberships(context.Background(), "", boss.ID)
	if len(memberships) != 1 || memberships[0].Role != core.RoleAdmin || memberships[0].EmployerID != "" {
		t.Fatalf("%+v", memberships)
	}
	org := memberships[0].Organization
	s.mustPost("/orgs/"+org+"/template", url.Values{"name": {"Full time"}, "mode": {"plan"}, "hours-1": {"8"}, "hours-2": {"8"}, "hours-3": {"8"},
		"hours-4": {"8"}, "hours-5": {"7"}, "leave_days": {"30"}, "basis": {"week"}, "back": {"/"}})

	worker := newSiteSharing(t, s)
	w := worker.me("tom", false)
	private := worker.employer(w, "My side job", core.ModeWeekSum, "")
	worker.mustPost("/book/week", url.Values{"employer": {private.ID}, "week": {monday.String()}, "hours": {"5"}, "back": {"/"}})
	worker.mustPost("/book/line", url.Values{"employer": {private.ID}, "line": {"Mi krank"}, "back": {"/"}})
	if a := worker.get("/orgs/" + org); a.status != http.StatusNotFound {
		t.Fatalf("a non-member sees the organization: %d", a.status)
	}
	if a := worker.post("/orgs", url.Values{"name": {"Mine"}}); a.status != http.StatusForbidden {
		t.Fatalf("a member founds organizations: %d", a.status)
	}

	s.mustPost("/orgs/"+org+"/member", url.Values{"person": {w.ID}, "role": {"member"}, "template": {"Full time"}, "from": {monday.String()},
		"records": {"on"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), w.ID)
	var managed core.Employer
	for _, e := range cat.Employers {
		if e.Managed() {
			managed = e
		}
	}
	if managed.ID == "" || managed.Mode != core.ModePlan || managed.Organization != org {
		t.Fatalf("no managed employer: %+v", cat.Employers)
	}
	home := worker.get("/?e=" + managed.ID).body
	if !strings.Contains(home, "Managed by ACME Construction") || !strings.Contains(home, "as planned") {
		t.Fatal("the member's week does not show the managed employer in its mode")
	}
	// The member books, but does not set the schedule or the pay.
	worker.mustPost("/book/adjust", url.Values{"employer": {managed.ID}, "date": {wednesday.String()}, "direction": {"more"}, "minutes": {"1:30"}, "back": {"/"}})
	if a := worker.post("/employers/"+managed.ID+"/pay", url.Values{"basis": {"hourly"}, "amount": {"99"}, "from": {"2026-01-01"}}); a.status != http.StatusBadRequest {
		t.Fatalf("a member sets the pay: %d", a.status)
	}
	if page := worker.get("/employers/" + managed.ID).body; strings.Contains(page, `action="/employers/`+managed.ID+`/pay"`) {
		t.Fatal("the member sees the form for the pay")
	}
	// The administrator sets it, for the member's employer.
	s.mustPost("/employers/"+managed.ID+"/pay", url.Values{"owner": {w.ID}, "basis": {"hourly"}, "amount": {"21"}, "from": {"2026-01-01"},
		"back": {"/"}})
	if cat, _ := s.store.Catalog(context.Background(), w.ID); len(cat.PayRules) != 1 || cat.PayRules[0].EmployerID != managed.ID {
		t.Fatalf("%+v", cat.PayRules)
	}
	// The administrator cannot reach the member's private employer.
	if a := s.get("/employers/" + private.ID + "?owner=" + w.ID); a.status != http.StatusNotFound {
		t.Fatalf("the organization reads a private employer: %d", a.status)
	}

	// Company features: off by default, on only with the agreement and the confirmation.
	page := s.get("/orgs/" + org).body
	if strings.Contains(page, "data-presence") || !strings.Contains(page, "All off") {
		t.Fatal("a company feature is on by default")
	}
	if a := s.post("/orgs/"+org+"/features", url.Values{"supervisor_view": {"on"}, "agreement": {"BV 2026"}}); a.status != http.StatusBadRequest {
		t.Fatalf("switched on without confirmation: %d", a.status)
	}
	if a := s.post("/orgs/"+org+"/features", url.Values{"supervisor_view": {"on"}, "confirm": {"on"}}); a.status != http.StatusBadRequest {
		t.Fatalf("switched on without the agreement: %d", a.status)
	}
	s.mustPost("/orgs/"+org+"/features", url.Values{"supervisor_view": {"on"}, "confirm": {"on"}, "agreement": {"BV 2026-11-01 v1"}, "back": {"/"}})
	saved, _ := s.store.Organization(context.Background(), org)
	if !saved.Features.SupervisorView || saved.Features.ConfirmedBy != boss.ID || saved.Features.ConfirmedAt == nil {
		t.Fatalf("%+v", saved.Features)
	}
	page = s.get("/orgs/" + org).body
	presence := regexp.MustCompile(`data-presence[\s\S]*?</div>\s*</section>`).FindString(page)
	if !strings.Contains(presence, "Mara tom") && !strings.Contains(presence, "tom") {
		t.Fatalf("the presence view does not list the member: %s", presence)
	}
	// Nothing private reaches the organization's page: the side job, its hours and the sickness booked there.
	for _, private := range []string{"My side job", "5:00", "Sick"} {
		if strings.Contains(page, private) {
			t.Errorf("the organization's page shows %q", private)
		}
	}
	if orgs := worker.get("/orgs").body; !strings.Contains(orgs, "presence view") || !strings.Contains(orgs, "private area stays untouched") {
		t.Fatal("the member is not told what the organization sees")
	}
	// The last administrator stays.
	if a := s.post("/orgs/"+org+"/role", url.Values{"membership": {memberships[0].ID}, "role": {"member"}}); a.status != http.StatusBadRequest {
		t.Fatalf("the last administrator lost the role: %d", a.status)
	}
	// Ending the membership archives the managed employer; its records stay.
	ms, _ := s.store.Memberships(context.Background(), org, w.ID)
	s.mustPost("/orgs/"+org+"/end", url.Values{"membership": {ms[0].ID}, "until": {wednesday.String()}, "back": {"/"}})
	cat, _ = s.store.Catalog(context.Background(), w.ID)
	if e, _ := cat.Employer(managed.ID); !e.Archived {
		t.Fatal("the employer of an ended membership is not archived")
	}
}

func TestParseHours(t *testing.T) {
	for input, want := range map[string]int{"": 0, "8": 480, "1,33": 80, "0,01": 1, "38:30": 2310, "38,5": 2310, "38.5": 2310, "7,25": 435, "7.75": 465,
		"38h30m": 2310, "38h": 2280, "90m": 90, "90min": 90, "-2": -120, "+1:30": 90, " 2h 15m ": 135} {
		if got, err := parseHours(input); err != nil || got != want {
			t.Errorf("%q: %d %v, want %d", input, got, err, want)
		}
	}
	for _, bad := range []string{"x", "1:75", "h", "1.234", "--1", "8:", ":30"} {
		if _, err := parseHours(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
	for input, want := range map[string]int64{"25": 2500, "25,5": 2550, "25.05": 2505, "1 200,00": 120000, "0,99": 99} {
		if got, err := parseMoney(input); err != nil || got != want {
			t.Errorf("money %q: %d %v", input, got, err)
		}
	}
	for _, bad := range []string{"", "-1", "1,234", "abc", "1.2.3"} {
		if _, err := parseMoney(bad); err == nil {
			t.Errorf("money %q passed", bad)
		}
	}
}
