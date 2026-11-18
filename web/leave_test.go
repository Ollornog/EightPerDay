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

// Leave is planned in the year's months: marked days with only a weekend between them become one absence, a half
// day counts half, and the account shows earned, taken, planned and left.
func TestLeavePlanning(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	emp := s.employer(me, "Office", core.ModeSchedule, "8")
	page := "/leave?e=" + emp.ID
	body := s.get(page).body
	// 30 days a year from 1 November on: 5 days; earned up to 18 November: 18 of 365 days of 30 is 1.5.
	if attr(body, "data-entitlement") != "5" || attr(body, "data-accrued") != "1.5" || attr(body, "data-remaining") != "5" {
		t.Fatalf("status %s %s %s", attr(body, "data-entitlement"), attr(body, "data-accrued"), attr(body, "data-remaining"))
	}
	if !strings.Contains(body, `data-pick="2026-12-18"`) || strings.Contains(body, `data-pick="2026-12-19"`) || strings.Contains(body, `data-pick="2026-12-25"`) {
		t.Error("only free working days can be marked: not a Saturday, not Christmas")
	}
	s.mustPost("/leave/plan", url.Values{"employer": {emp.ID}, "back": {page}, "d": {"2026-12-21", "2026-12-18", "2026-12-22"}})
	s.mustPost("/leave/plan", url.Values{"employer": {emp.ID}, "back": {page}, "d": {"2026-12-24", "2026-12-31"}, "half": {"on"}})
	absences := s.absences(me.ID)
	if len(absences) != 3 || absences[0].From != core.MustDate("2026-12-18") || absences[0].To != core.MustDate("2026-12-22") ||
		absences[0].Type != core.AbsenceVacation || !absences[1].HalfDay || absences[1].From != absences[1].To || !absences[2].HalfDay {
		t.Fatalf("absences %+v", absences)
	}
	body = s.get(page).body
	if attr(body, "data-planned") != "4" || attr(body, "data-taken") != "0" || attr(body, "data-remaining") != "1" {
		t.Errorf("planned %s taken %s left %s", attr(body, "data-planned"), attr(body, "data-taken"), attr(body, "data-remaining"))
	}
	if strings.Contains(body, `data-pick="2026-12-21"`) || !strings.Contains(body, `data-cell="2026-12-24"`) {
		t.Error("planned days are no longer free to mark")
	}
	if a := s.post("/leave/plan", url.Values{"employer": {emp.ID}, "back": {page}}); a.status != http.StatusBadRequest {
		t.Errorf("nothing marked: %d", a.status)
	}
	if a := s.post("/leave/plan", url.Values{"employer": {emp.ID}, "back": {page}, "d": {"someday"}}); a.status != http.StatusBadRequest {
		t.Errorf("a bad day: %d", a.status)
	}
}

// On an organization that approves leave, marked days are asked for; the reviewer sees who else is away then, and
// never a sickness.
func TestLeaveRequestShowsWhoElseIsAway(t *testing.T) {
	s, worker, org, member, managed := approvalSite(t)
	other := newSiteSharing(t, s)
	ann := other.me("ann", false)
	s.mustPost("/orgs/"+org+"/member", url.Values{"person": {ann.ID}, "role": {"member"}, "template": {"Full time"},
		"from": {"2026-11-01"}, "records": {"on"}, "back": {"/"}})
	cat, _ := s.store.Catalog(context.Background(), ann.ID)
	var annEmp core.Employer
	for _, e := range cat.Employers {
		if e.Managed() {
			annEmp = e
		}
	}
	other.mustPost("/leave/plan", url.Values{"employer": {annEmp.ID}, "back": {"/leave"}, "d": {"2026-12-21", "2026-12-22"}})
	other.mustPost("/book/absence", url.Values{"employer": {annEmp.ID}, "type": {"sick"}, "from": {"2026-12-23"}, "back": {"/"}})
	page := "/leave?e=" + managed.ID
	if !strings.Contains(worker.get(page).body, "Ask for leave") {
		t.Error("the page does not say the leave is asked for")
	}
	worker.mustPost("/leave/plan", url.Values{"employer": {managed.ID}, "back": {page}, "d": {"2026-12-22", "2026-12-23"}})
	if a := worker.absences(member.ID); len(a) != 1 || a[0].Status != core.StatusRequested {
		t.Fatalf("the leave is not requested: %+v", a)
	}
	body := s.get("/orgs/" + org).body
	// Both requests name the other: tom's names ann's leave, never her sickness on the 23rd.
	var overlaps string
	for _, block := range regexp.MustCompile(`data-overlaps>[\s\S]*?</div>`).FindAllString(body, -1) {
		if strings.Contains(block, "ann:") {
			overlaps = block
		}
	}
	if !strings.Contains(overlaps, "ann: 2026-12-21 – 2026-12-22 (waits for approval)") || strings.Count(overlaps, "<li>") != 1 {
		t.Errorf("who else is away: %s", overlaps)
	}
}
