// SPDX-License-Identifier: MIT

package web

import (
	"context"
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
