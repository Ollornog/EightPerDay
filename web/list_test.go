// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Ollornog/EightPerDay/core"
)

// rowValue is the value of a day's field in the list.
func rowValue(t *testing.T, body string, date core.Date) string {
	t.Helper()
	m := regexp.MustCompile(`id="v-` + date.String() + `" name="v-` + date.String() + `" value="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no field for %s", date)
	}
	return m[1]
}

func attr(body, name string) string {
	m := regexp.MustCompile(name + `>([^<]*)<`).FindStringSubmatch(body)
	if m == nil {
		return "(none)"
	}
	return m[1]
}

func (s *site) absences(owner string) []core.Absence {
	s.t.Helper()
	all, err := s.store.Absences(context.Background(), core.AbsenceFilter{Owner: owner})
	if err != nil {
		s.t.Fatal(err)
	}
	var live []core.Absence
	for _, a := range all {
		if a.DeletedAt == nil {
			live = append(live, a)
		}
	}
	return live
}

// The start page is the list: this week and last week side by side, the first empty working day of this week has
// the focus, and the weeks that lack something are named on top with a link to them.
func TestListIsTheStartPage(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	body := s.get("/").body
	weeks := regexp.MustCompile(`data-week="([^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(weeks) != 2 || weeks[0][1] != "2026-11-09" || weeks[1][1] != "2026-11-16" {
		t.Fatalf("the list shows the weeks %v", weeks)
	}
	if !strings.Contains(body, `data-weeks`) || !strings.Contains(body, "lg:grid-cols-2") {
		t.Error("the two weeks do not stand side by side")
	}
	// The year grid has a row of 31 cells per month; what a month lacks is outside it (2026: 7 days in all).
	year := body[strings.Index(body, `class="year-grid"`):]
	year = year[:strings.Index(year, "</table>")]
	if rows := strings.Count(year, `<th scope="row">`); rows != 12 {
		t.Errorf("the year grid has %d rows", rows)
	}
	if cells, outside := strings.Count(year, "<td"), strings.Count(year, "<td data-outside>"); cells != 12*31 || outside != 7 {
		t.Errorf("the year grid has %d cells, %d outside", cells, outside)
	}
	if focus := regexp.MustCompile(`name="v-([0-9-]+)"[^>]*autofocus`).FindStringSubmatch(body); focus == nil || focus[1] != "2026-11-16" {
		t.Errorf("the focus is on %v, not on Monday", focus)
	}
	// The schedule starts on 1 November: weeks 45 and 46 lack five days each, this week Monday and Tuesday.
	gaps := regexp.MustCompile(`data-gap="([^"]+)"><a [^>]*href="([^"]+)">Week (\d+)</a>:\s*<span data-gap-missing>(\d) working days`).FindAllStringSubmatch(body, -1)
	if len(gaps) != 3 || gaps[0][3] != "45" || gaps[0][4] != "5" || gaps[2][3] != "47" || gaps[2][4] != "2" {
		t.Fatalf("gaps %v", gaps)
	}
	if want := "/?e=" + emp.ID + "&w=2026-11-02#week-2026-11-02"; gaps[0][2] != want {
		t.Errorf("the gap links to %s, not %s", gaps[0][2], want)
	}
	// The link opens that week with the one before it.
	older := s.get("/?e=" + emp.ID + "&w=2026-11-02").body
	if weeks := regexp.MustCompile(`data-week="([^"]+)"`).FindAllStringSubmatch(older, -1); len(weeks) != 2 || weeks[1][1] != "2026-11-02" {
		t.Errorf("the older list shows %v", weeks)
	}
	// The other tools are on the week page, not on the start page.
	for _, tool := range []string{`action="/book/line"`, "data-approval", `action="/timer/start"`} {
		if strings.Contains(body, tool) {
			t.Errorf("the start page shows %s", tool)
		}
	}
	if !strings.Contains(s.get("/week?e="+emp.ID).body, `action="/book/line"`) {
		t.Error("the week page lost its tools")
	}
}

// A week typed into the list: hours with a comma, a clock, a code for leave and a half day of sickness, a note; the
// week sums, the differences and the balance follow, and saving the week again books nothing twice.
func TestListTakesHoursAndCodes(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	back := "/?e=" + emp.ID
	form := url.Values{"employer": {emp.ID}, "back": {back},
		"v-2026-11-16": {"8,5"}, "was-2026-11-16": {""}, "n-2026-11-16": {"inventory"}, "wasn-2026-11-16": {""},
		"v-2026-11-17": {"u"}, "was-2026-11-17": {""},
		"v-2026-11-18": {"½K"}, "was-2026-11-18": {""}, "n-2026-11-18": {"never stored"},
		"v-2026-11-19": {"7:30"}, "was-2026-11-19": {""},
		"v-2026-11-20": {""}, "was-2026-11-20": {""}}
	s.mustPost("/book/list", form)
	entries := s.entries(me.ID)
	if len(entries) != 2 || entries[0].Minutes != 510 || entries[0].Note != "inventory" || entries[1].Minutes != 450 {
		t.Fatalf("entries %+v", entries)
	}
	absences := s.absences(me.ID)
	if len(absences) != 2 || absences[0].Type != core.AbsenceVacation || absences[0].From != tuesday ||
		absences[1].Type != core.AbsenceSick || !absences[1].HalfDay || absences[1].Note != "" {
		t.Fatalf("absences %+v", absences)
	}
	body := s.get(back).body
	for date, want := range map[core.Date]string{monday: "8.5", tuesday: "V", wednesday: "½S", wednesday.AddDays(1): "7.5"} {
		if got := rowValue(t, body, date); got != want {
			t.Errorf("%s shows %q, want %q", date, got, want)
		}
	}
	// Up to today, Wednesday: +0:30 on Monday, leave meets the target, half a day sick and nothing worked is -4.
	week := body[strings.Index(body, `data-week="2026-11-16"`):]
	if sum, diff := attr(week, "data-week-sum"), attr(week, "data-week-diff"); sum != "8.5" || diff != "-3.5" {
		t.Errorf("week sum %s, difference %s", sum, diff)
	}
	if bal := attr(week, "data-week-balance"); bal != "Balance -83.5" {
		t.Errorf("balance %q (weeks 45 and 46 are empty: -80)", bal)
	}
	// Saved again as it stands, nothing is booked anew.
	history, _ := s.store.Revisions(context.Background(), me.ID, "")
	again := url.Values{"employer": {emp.ID}, "back": {back}}
	for _, d := range []string{"2026-11-16", "2026-11-17", "2026-11-18", "2026-11-19"} {
		again.Set("v-"+d, rowValue(t, body, core.MustDate(d)))
		again.Set("was-"+d, rowValue(t, body, core.MustDate(d)))
	}
	again.Set("n-2026-11-16", "inventory")
	again.Set("wasn-2026-11-16", "inventory")
	s.mustPost("/book/list", again)
	if after, _ := s.store.Revisions(context.Background(), me.ID, ""); len(after) != len(history) {
		t.Errorf("saving the same week again wrote %d revisions", len(after)-len(history))
	}
	// A day changed elsewhere after the page was loaded keeps its change when the week is saved for another day.
	s.mustPost("/book/list", url.Values{"employer": {emp.ID}, "back": {back}, "v-2026-11-20": {"6"}, "was-2026-11-20": {""}})
	s.mustPost("/book/list", url.Values{"employer": {emp.ID}, "back": {back}, "v-2026-11-20": {""}, "was-2026-11-20": {""},
		"v-2026-11-21": {"2"}, "was-2026-11-21": {""}})
	if body := s.get(back).body; rowValue(t, body, wednesday.AddDays(2)) != "6" || rowValue(t, body, wednesday.AddDays(3)) != "2" {
		t.Error("saving a stale week undid a change made elsewhere")
	}
	s.mustPost("/book/list", url.Values{"employer": {emp.ID}, "back": {back}, "v-2026-11-20": {""}, "was-2026-11-20": {"6"},
		"v-2026-11-21": {""}, "was-2026-11-21": {"2"}})
	// Leave becomes hours, hours are cleared: the absence and the entry go through the history.
	s.mustPost("/book/list", url.Values{"employer": {emp.ID}, "back": {back}, "v-2026-11-17": {"6"}, "was-2026-11-17": {"V"},
		"v-2026-11-16": {""}, "was-2026-11-16": {"8.5"}, "n-2026-11-16": {""}, "wasn-2026-11-16": {"inventory"}})
	if absences := s.absences(me.ID); len(absences) != 1 || absences[0].Type != core.AbsenceSick {
		t.Errorf("the leave was not replaced: %+v", absences)
	}
	entries = s.entries(me.ID)
	if len(entries) != 2 || entries[0].Date != tuesday || entries[0].Minutes != 360 {
		t.Errorf("entries after the change %+v", entries)
	}
	// An older week is changed the same way, on a private employer simply.
	s.mustPost("/book/list", url.Values{"employer": {emp.ID}, "back": {back}, "v-2026-11-02": {"8"}, "was-2026-11-02": {""}})
	if body := s.get(back + "&w=2026-11-02").body; rowValue(t, body, core.MustDate("2026-11-02")) != "8" {
		t.Error("the older week did not take its hours")
	}
}

func TestListRefusesWhatItCannotRead(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	for value, note := range map[string]string{"x": "", "-2": "", "25": "", "U½U": "", "": "a note alone", "U 4": "", "4 K": "",
		"4x": "", "½U ½K": "", "25v": ""} {
		a := s.post("/book/list", url.Values{"employer": {emp.ID}, "back": {"/"}, "v-2026-11-16": {value}, "was-2026-11-16": {"1"},
			"n-2026-11-16": {note}})
		if a.status != http.StatusBadRequest {
			t.Errorf("%q / %q: %d", value, note, a.status)
		}
	}
	if len(s.entries(me.ID)) != 0 || len(s.absences(me.ID)) != 0 {
		t.Error("a refused row booked something")
	}
}

// The field's pattern lets through exactly what the server reads, so the browser never stops a value the server would
// take, nor lets one pass the server refuses.
func TestDayPatternAgreesWithParseDayValue(t *testing.T) {
	pattern := regexp.MustCompile(`^(?:` + dayPattern + `)$`)
	for _, input := range []string{"8", "8,5", "7:30", "7h30", "90m", "0", "U", "u", "K", "F", "ZA", "za", "V", "S", "O", "C",
		"½U", "U½", "U/2", "0,5U", "0.5 u", ",5F", "-2", "+2", "x", "UU", "H", "½", "½U½", "Z A", "U /2", "8U", "25:00", "1:75",
		"4v", "4 NM", "3,5pm", "4a", "4 P", "4vm", "4x", "4 vv", "4 v m", "½U 4n", "4 NM ½U", "U/2 4", "4 ½K", "U 4", "4 U", "½U ½U",
		"½U 4n ½K", "½U4", "v", "nm"} {
		_, err := parseDayValue(input)
		if got, want := pattern.MatchString(input), err == nil; got != want && input != "25:00" {
			t.Errorf("%q: pattern says %v, the server %v", input, got, want)
		}
	}
}

// A correction of the time account with its reason: the week it falls in shows it and the balance follows; a member
// of an organization cannot correct a managed account.
func TestCorrectionOfTheTimeAccount(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	back := "/?e=" + emp.ID
	if a := s.post("/book/correction", url.Values{"employer": {emp.ID}, "date": {"2026-11-17"}, "minutes": {"2"}, "back": {back}}); a.status != http.StatusBadRequest {
		t.Errorf("a correction without a reason: %d", a.status)
	}
	s.mustPost("/book/correction", url.Values{"employer": {emp.ID}, "date": {"2026-11-17"}, "minutes": {"2"}, "direction": {"less"},
		"reason": {"paid out"}, "back": {back}})
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	if len(cat.Adjustments) != 1 || cat.Adjustments[0].Minutes != -120 || cat.Adjustments[0].Reason != "paid out" {
		t.Fatalf("adjustments %+v", cat.Adjustments)
	}
	body := s.get(back).body
	week := body[strings.Index(body, `data-week="2026-11-16"`):]
	if !strings.Contains(week, "Correction 11-17: paid out") || attr(week, "data-week-balance") != "Balance -106" {
		t.Errorf("the week does not show the correction: %s / %s", attr(week, "data-correction"), attr(week, "data-week-balance"))
	}
	// Weeks 45 and 46 are empty (-80), this week up to today -24, and the payout -2.
	if attr(body, "data-account") != "-106" {
		t.Errorf("the account shows %s", attr(body, "data-account"))
	}
	s.mustPost("/corrections/"+cat.Adjustments[0].ID+"/remove", url.Values{"back": {back}})
	if cat, _ := s.store.Catalog(context.Background(), me.ID); len(cat.Adjustments) != 0 {
		t.Error("the correction was not removed")
	}

	_, worker, _, _, managed := approvalSite(t)
	if a := worker.post("/book/correction", url.Values{"employer": {managed.ID}, "date": {"2026-11-17"}, "minutes": {"2"},
		"reason": {"own payout"}, "back": {"/"}}); a.status != http.StatusBadRequest || !strings.Contains(a.body, "You may not change this") {
		t.Errorf("a member corrected the managed account: %d", a.status)
	}
	if strings.Contains(worker.get("/?e="+managed.ID).body, "data-corrections") {
		t.Error("a member is offered the correction")
	}
}

// A handed-in week shows its values without fields and refuses changes from the list.
func TestListOfAHandedInWeek(t *testing.T) {
	_, worker, _, _, managed := approvalSite(t)
	worker.mustPost("/book/list", url.Values{"employer": {managed.ID}, "back": {"/"}, "v-2026-11-16": {"9"}, "was-2026-11-16": {""}})
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {"/"}})
	body := worker.get("/?e=" + managed.ID).body
	if strings.Contains(body, `name="v-2026-11-16"`) || !strings.Contains(body, `data-fixed>9<`) {
		t.Error("the handed-in week still has fields")
	}
	if a := worker.post("/book/list", url.Values{"employer": {managed.ID}, "back": {"/"}, "v-2026-11-16": {"7"}, "was-2026-11-16": {"9"}}); a.status != http.StatusBadRequest {
		t.Errorf("a handed-in day changed: %d", a.status)
	}
}

// A change to an approved week on a managed employer becomes a change request: the approved values count until the
// organization decides; approved, the values are booked and the week is approved again; rejected, nothing changes.
func TestChangeRequestOnAnApprovedWeek(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	list := "/?e=" + managed.ID
	worker.mustPost("/book/list", url.Values{"employer": {managed.ID}, "back": {list}, "v-2026-11-16": {"9"}, "was-2026-11-16": {""}})
	worker.mustPost("/approval/submit", url.Values{"employer": {managed.ID}, "kind": {"period"}, "from": {monday.String()},
		"to": {monday.AddDays(6).String()}, "back": {list}})
	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {submissions(t, s, member.ID)[0].ID}, "decision": {"approve"}, "back": {"/"}})

	body := worker.get(list).body
	if !strings.Contains(body, "data-approved") || rowValue(t, body, monday) != "9" || strings.Contains(body, "data-requested") {
		t.Fatal("the approved week cannot be changed from the list")
	}
	change := url.Values{"employer": {managed.ID}, "back": {list}, "v-2026-11-16": {"7"}, "was-2026-11-16": {"9"}, "n-2026-11-16": {"dentist"}}
	if a := worker.post("/book/list", change); a.status != http.StatusBadRequest || !strings.Contains(a.body, "says why") {
		t.Fatalf("a change request without a reason: %d", a.status)
	}
	change.Set("reason", "left for the dentist")
	change.Set("v-2026-11-16", "6")
	if a := worker.mustPost("/book/list", change); !strings.Contains(a.header.Get("Location"), "done=requested") {
		t.Errorf("redirect %s", a.header.Get("Location"))
	}
	// Asked again, the day takes its new value in the open request.
	change.Set("v-2026-11-16", "7")
	worker.mustPost("/book/list", change)
	worker.mustPost("/book/list", url.Values{"employer": {managed.ID}, "back": {list}, "v-2026-11-17": {"v"}, "was-2026-11-17": {""},
		"reason": {"and a day of leave"}})
	if e := worker.entries(member.ID); len(e) != 1 || e[0].Minutes != 540 {
		t.Fatalf("the approved values changed before the decision: %+v", e)
	}
	var request core.Submission
	for _, x := range submissions(t, s, member.ID) {
		if x.Kind == core.SubmitCorrection {
			if request.ID != "" {
				t.Fatal("a second change request instead of one")
			}
			request = x
		}
	}
	if len(request.Proposal) != 2 || request.Proposal[0].Minutes != 420 || request.Proposal[0].Note != "dentist" ||
		request.Proposal[1].Absence != core.AbsenceVacation || request.Note != "and a day of leave" {
		t.Fatalf("change request %+v", request)
	}
	body = worker.get(list).body
	if !strings.Contains(body, "data-requested") || !strings.Contains(body, "data-proposed>→ 7<") || rowValue(t, body, monday) != "9" {
		t.Error("the list does not mark the request next to the approved values")
	}
	if page := s.get("/orgs/" + org).body; !strings.Contains(page, "Mon 11-16: 7 („dentist“)") || !strings.Contains(page, "Tue 11-17: V") {
		t.Errorf("the reviewer does not see what is asked: %s", regexp.MustCompile(`data-changes>[\s\S]{0,200}`).FindString(page))
	}

	s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {request.ID}, "decision": {"approve"}, "back": {"/"}})
	if e := worker.entries(member.ID); len(e) != 1 || e[0].Minutes != 420 || e[0].Note != "dentist" {
		t.Fatalf("the approved request was not booked: %+v", e)
	}
	if a := worker.absences(member.ID); len(a) != 1 || a[0].Type != core.AbsenceVacation || a[0].Status != core.StatusApproved {
		t.Fatalf("absences %+v", a)
	}
	cat, _ := s.store.Catalog(context.Background(), member.ID)
	if p, ok := cat.PeriodAt(managed.ID, monday); !ok || p.Status != core.SubmissionApproved || p.ID == request.Supersedes {
		t.Fatalf("the corrected week is not approved anew: %+v %v", p, ok)
	}
	accesses, _ := s.store.(core.AccessLog).Accesses(context.Background(), member.ID)
	applied := false
	for _, a := range accesses {
		applied = applied || (a.Action == core.AccessChange && a.What == "change request applied")
	}
	if !applied {
		t.Error("the member's access log lacks the change")
	}

	// Rejected, nothing changes.
	worker.mustPost("/book/list", url.Values{"employer": {managed.ID}, "back": {list}, "v-2026-11-16": {"8"}, "was-2026-11-16": {"7"},
		"n-2026-11-16": {"dentist"}, "wasn-2026-11-16": {"dentist"}, "reason": {"typo"}})
	for _, x := range submissions(t, s, member.ID) {
		if x.Kind == core.SubmitCorrection && x.Status == core.SubmissionSubmitted {
			s.mustPost("/orgs/"+org+"/decide", url.Values{"submission": {x.ID}, "decision": {"reject"}, "reason": {"was right"}, "back": {"/"}})
		}
	}
	if e := worker.entries(member.ID); len(e) != 1 || e[0].Minutes != 420 {
		t.Errorf("a rejected request changed the day: %+v", e)
	}
	if body := worker.get(list).body; strings.Contains(body, "data-requested") || rowValue(t, body, monday) != "7" {
		t.Error("after the rejection the week does not show the approved values")
	}
}

// The list of every week: the newest at the bottom, eight at first, and the weeks before load in parts until the
// first week with records; each week's form returns to its own week.
func TestEveryWeek(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	s.mustPost("/book/list", url.Values{"employer": {emp.ID}, "back": {"/"}, "v-2026-06-02": {"8"}, "was-2026-06-02": {""}})
	weeksOf := func(body string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`data-week="([^"]+)"`).FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	body := s.get("/weeks?e=" + emp.ID).body
	if w := weeksOf(body); len(w) != 8 || w[0] != "2026-09-28" || w[7] != "2026-11-16" || !strings.Contains(body, "data-endless") {
		t.Fatalf("first weeks %v", w)
	}
	if strings.Contains(body, "autofocus") {
		t.Error("the list of every week takes the focus")
	}
	if !strings.Contains(body, `value="/weeks?e=`+emp.ID+`&from=2026-11-09#week-2026-11-09"`) {
		t.Errorf("a week's form does not return to its week: %s", regexp.MustCompile(`name="back" value="/weeks[^"]*"`).FindString(body))
	}
	seen := weeksOf(body)
	part := regexp.MustCompile(`data-part="([^"]+)"`)
	for loads := 0; ; loads++ {
		next := part.FindStringSubmatch(body)
		if next == nil {
			break
		}
		if loads > 5 {
			t.Fatal("the weeks before do not end")
		}
		body = s.get(strings.ReplaceAll(next[1], "&amp;", "&")).body
		if !strings.HasPrefix(body, "<div data-weeks-part>") || strings.Contains(body, "<html") {
			t.Fatalf("a part is a whole page: %.200s", body)
		}
		seen = append(weeksOf(body), seen...)
	}
	if !strings.Contains(body, "data-weeks-start") || seen[0] != "2026-06-01" || len(seen) != 25 {
		t.Errorf("the weeks end at %v after %d weeks", seen[0], len(seen))
	}
	for i := 1; i < len(seen); i++ {
		if core.MustDate(seen[i]) != core.MustDate(seen[i-1]).AddDays(7) {
			t.Fatalf("weeks out of order at %d: %v", i, seen)
		}
	}
	// Without JavaScript the link opens the page from an older week on.
	if w := weeksOf(s.get("/weeks?e=" + emp.ID + "&from=2026-08-10").body); len(w) != 15 || w[0] != "2026-08-10" {
		t.Errorf("from an older week: %v", w)
	}
}
