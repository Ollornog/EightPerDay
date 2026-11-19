// SPDX-License-Identifier: MIT

package core

import (
	"testing"
	"time"
)

func TestCalendarAbsences(t *testing.T) {
	gone := time.Now()
	leave := absence(AbsenceVacation, "2026-08-03", "2026-08-14")
	sick := absence(AbsenceSick, "2026-03-02", "2026-03-03")
	asked := absence(AbsenceVacation, "2026-12-28", "2026-12-31")
	asked.Status, asked.HalfDay = StatusRequested, true
	rejected := absence(AbsenceVacation, "2026-05-04", "2026-05-04")
	rejected.Status = StatusRejected
	deleted := absence(AbsenceVacation, "2026-06-01", "2026-06-01")
	deleted.DeletedAt = &gone
	everyEmployer := absence(AbsenceVacation, "2026-01-02", "2026-01-02")
	everyEmployer.EmployerID = ""
	outside := absence(AbsenceVacation, "2025-12-01", "2025-12-05")
	data := Data{Catalog: Catalog{Employers: []Employer{employerOf(ModeSchedule, "DE-BY")}},
		Absences: []Absence{leave, sick, asked, rejected, deleted, everyEmployer, outside}}

	got := CalendarAbsences(data, d("2026-01-01"), d("2026-12-31"))
	if len(got) != 4 {
		t.Fatalf("want 4 absences, got %+v", got)
	}
	want := []string{everyEmployer.ID, sick.ID, leave.ID, asked.ID}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("order: %d is %s, want %s (%+v)", i, got[i].ID, id, got)
		}
	}
	if c := got[2]; c.Employer != "Example GmbH" || !c.Leave || c.Health || c.Status != StatusApproved || c.Name == "" {
		t.Errorf("leave: %+v", c)
	}
	if c := got[1]; !c.Health || c.Leave {
		t.Errorf("sickness is health data and no leave: %+v", c)
	}
	if c := got[3]; c.Status != StatusRequested || !c.HalfDay {
		t.Errorf("requested half days: %+v", c)
	}
	if c := got[0]; c.Employer != "" || c.EmployerID != "" {
		t.Errorf("every employer: %+v", c)
	}
	// Open ends take everything that is neither rejected nor deleted.
	if all := CalendarAbsences(data, Date{}, Date{}); len(all) != 5 {
		t.Errorf("open range: %d", len(all))
	}
	// One day inside a span finds it.
	if one := CalendarAbsences(data, d("2026-08-10"), d("2026-08-10")); len(one) != 1 || one[0].ID != leave.ID {
		t.Errorf("a day inside: %+v", one)
	}
}
