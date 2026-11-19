// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Ollornog/EightPerDay/core"
)

// Three jobs of a season in one list: a taxi paid by the hour, ski lessons by the day, tandem flights by the job with
// photos and tips taken in cash or by card. Each employer shows only on its days; the week sums the earnings and the
// average working day.
func TestAllEmployersInOneWeek(t *testing.T) {
	s, _ := trackSite(t)
	me := s.me("mara", false)
	taxi := s.employer(me, "Taxi", core.ModeSchedule, "")
	ski := s.employer(me, "Ski school", core.ModeSchedule, "")
	flights := s.employer(me, "Tandem flights", core.ModeSchedule, "")
	s.mustPost("/employers/"+taxi.ID+"/pay", url.Values{"basis": {"hourly"}, "amount": {"20"}, "from": {"2026-01-01"}, "back": {"/"}})
	s.mustPost("/employers/"+ski.ID+"/pay", url.Values{"basis": {"per_assignment"}, "unit": {"day"}, "amount": {"120"}, "from": {"2026-01-01"}, "back": {"/"}})
	s.mustPost("/book/list", url.Values{"employer": {taxi.ID}, "back": {"/"}, "v-2026-11-16": {"4"}, "was-2026-11-16": {""}})
	s.mustPost("/book/list", url.Values{"employer": {ski.ID}, "back": {"/"}, "v-2026-11-17": {"6"}, "was-2026-11-17": {""}})
	income := func(kind, amount, by, client string) {
		s.mustPost("/book/income", url.Values{"employer": {flights.ID}, "back": {"/"}, "date": {"2026-11-17"}, "kind": {kind},
			"amount": {amount}, "paid_by": {by}, "client": {client}})
	}
	income("job", "110", "card", "a guest")
	income("photos", "30", "cash", "")
	income("tip", "10", "cash", "")
	s.mustPost("/book/dayinfo", url.Values{"employer": {ski.ID}, "back": {"/"}, "date": {"2026-11-17"}, "place": {"Valley"},
		"km_one_way": {"12,5"}, "km_total": {"25"}, "standby_passive": {""}, "standby_active": {""}})

	body := s.get("/all?w=2026-11-16").body
	if n := strings.Count(body, "data-column="); n != 3 {
		t.Fatalf("%d columns", n)
	}
	monday := body[strings.Index(body, `data-all-day="2026-11-16"`):strings.Index(body, `data-all-day="2026-11-17"`)]
	if strings.Count(monday, "data-hours") != 1 || !strings.Contains(monday, "80.00 EUR") {
		t.Errorf("Monday shows only the taxi: %s", monday)
	}
	tuesday := body[strings.Index(body, `data-all-day="2026-11-17"`):strings.Index(body, `data-all-day="2026-11-18"`)]
	if strings.Count(tuesday, "data-hours") != 2 || attr(tuesday, "data-day-earned") != "270.00 EUR" || !strings.Contains(tuesday, "Valley · 25 km") {
		t.Errorf("Tuesday: %s", tuesday)
	}
	for name, want := range map[string]string{"data-week-earned": "350.00 EUR", "data-average": "175.00 EUR", "data-cash": "40.00 EUR",
		"data-card": "110.00 EUR"} {
		if got := attr(body, name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	// A week without anything shows no column.
	if strings.Contains(s.get("/all?w=2026-11-02").body, "data-column=") {
		t.Error("an idle week shows employers")
	}
	// The day keeps its details, and they go when all are cleared.
	if day := s.get("/day?e=" + ski.ID + "&d=2026-11-17").body; !strings.Contains(day, `value="12.5"`) || !strings.Contains(day, `value="Valley"`) {
		t.Error("the day does not show its details")
	}
	s.mustPost("/book/dayinfo", url.Values{"employer": {ski.ID}, "back": {"/"}, "date": {"2026-11-17"}})
	if cat, _ := s.store.Catalog(context.Background(), me.ID); len(cat.DayInfos) != 0 || len(cat.Incomes) != 3 {
		t.Errorf("details %+v, incomes %d", cat.DayInfos, len(cat.Incomes))
	}
	if a := s.post("/book/dayinfo", url.Values{"employer": {ski.ID}, "back": {"/"}, "date": {"2026-11-17"}, "km_total": {"far"}}); a.status != 400 {
		t.Errorf("kilometres as words: %d", a.status)
	}
	cat, _ := s.store.Catalog(context.Background(), me.ID)
	s.mustPost("/incomes/"+cat.Incomes[0].ID+"/remove", url.Values{"back": {"/"}})
	if cat, _ := s.store.Catalog(context.Background(), me.ID); len(cat.Incomes) != 2 {
		t.Error("the income was not removed")
	}
	if !strings.Contains(s.get("/").body, "data-all-employers") {
		t.Error("the list does not lead to all employers")
	}
}
