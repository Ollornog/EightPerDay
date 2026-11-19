// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

// The leave page: the account of the year as of today (core.LeaveAt) and the year as twelve months, in which free
// working days are marked and booked as planned leave in one step. On an organization that approves leave, the leave
// is requested and handed in at once (createAbsence), as on the week page.

type leaveData struct {
	Employers  []core.Employer
	Employer   core.Employer
	Year       int
	Prev, Next int
	Today      core.Date
	Status     core.LeaveStatus
	HasLeave   bool
	Months     []leaveMonth
	Absences   []absenceView
	Approval   bool
	Back       string
}

type leaveMonth struct {
	Month time.Month
	Weeks [][]leaveCell
}

type leaveCell struct {
	Date    core.Date
	Day     int
	In      bool // the cell belongs to the month
	Code    string
	Pick    bool // a free working day that can be marked
	Weekend bool
	Today   bool
}

func (h *handler) leavePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	emp, ok := h.pickEmployer(w, r, cat)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	today := h.today(emp)
	year := today.Year
	if y, err := strconv.Atoi(r.URL.Query().Get("y")); err == nil && y >= 2000 && y <= 2100 {
		year = y
	}
	d := leaveData{Employers: activeEmployers(cat), Employer: *emp, Year: year, Prev: year - 1, Next: year + 1, Today: today,
		Back: "/leave?e=" + emp.ID + "&y=" + strconv.Itoa(year)}
	first, last := core.NewDate(year, time.January, 1), core.NewDate(year, time.December, 31)
	data, err := core.Load(ctx, h.options.Store, emp.Owner, first, last)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	if d.Status, err = core.LeaveAt(data, emp.ID, year, today); err != nil {
		h.failed(w, r, err)
		return
	}
	d.HasLeave = d.Status.Entitlement > 0 || d.Status.Taken > 0 || d.Status.Requested > 0
	if _, d.Approval, err = h.approvalOrg(ctx, *emp); err != nil {
		h.failed(w, r, err)
		return
	}
	days, err := core.Days(data, emp.ID, first, last)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	codes := h.dayCodes(r, cat, *emp, data)
	cells := map[core.Date]leaveCell{}
	for _, day := range days {
		weekend := day.Date.Weekday() == time.Saturday || day.Date.Weekday() == time.Sunday
		c := leaveCell{Date: day.Date, Day: day.Date.Day, In: true, Code: codes[day.Date].text, Weekend: weekend, Today: day.Date == today}
		if day.Holiday != "" && c.Code == "" {
			c.Code = h.t(r, "code.public")
		}
		working := day.Target > 0 || (!day.HasTarget && !weekend)
		c.Pick = working && c.Code == "" && !cat.SubmittedLocked(emp.ID, day.Date)
		cells[day.Date] = c
	}
	for m := time.January; m <= time.December; m++ {
		lm := leaveMonth{Month: m}
		start := core.NewDate(year, m, 1).StartOfWeek(emp.WeekStart)
		for week := start; week.Month == m || week.Before(core.NewDate(year, m, 1)); week = week.AddDays(7) {
			var row []leaveCell
			for i := range 7 {
				day := week.AddDays(i)
				c, ok := cells[day]
				if !ok || day.Month != m {
					c = leaveCell{Date: day, Day: day.Day}
				}
				row = append(row, c)
			}
			lm.Weeks = append(lm.Weeks, row)
		}
		d.Months = append(d.Months, lm)
	}
	names := map[string]string{}
	for _, t := range cat.AbsenceTypesFor(emp.ID) {
		if t.DeductsLeave {
			names[t.Key] = h.absenceName(r, t)
		}
	}
	for _, a := range data.Absences {
		if _, leave := names[a.Type]; leave && a.DeletedAt == nil && (a.EmployerID == emp.ID || (a.EmployerID == "" && !emp.Managed())) {
			d.Absences = append(d.Absences, absenceView{Absence: a, Name: names[a.Type]})
		}
	}
	slices.SortFunc(d.Absences, func(a, b absenceView) int { return a.From.Compare(b.From) })
	h.render(w, r, http.StatusOK, "leave", view{Title: h.t(r, "leave.title"), Data: d, Wide: true})
}

// planLeave books the marked days as leave: days next to each other, or with only days off between them, become one
// absence.
func (h *handler) planLeave(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.back(w, r, err, "")
		return
	}
	var dates []core.Date
	for _, raw := range r.PostForm["d"] {
		date, err := core.ParseDate(raw)
		if err != nil {
			h.back(w, r, core.ErrInvalid, "")
			return
		}
		if !slices.Contains(dates, date) {
			dates = append(dates, date)
		}
	}
	if len(dates) == 0 {
		h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "leave.error.none"))
		return
	}
	slices.SortFunc(dates, func(a, b core.Date) int { return a.Compare(b) })
	half := r.PostFormValue("half") == "on"
	for _, span := range leaveSpans(cat, emp, dates) {
		a := core.Absence{Owner: emp.Owner, EmployerID: emp.ID, Type: core.AbsenceVacation, From: span[0], To: span[1], HalfDay: half}
		if err := h.createAbsence(r, cat, emp, a); err != nil {
			h.back(w, r, err, "")
			return
		}
	}
	h.back(w, r, nil, "planned")
}

// leaveSpans groups sorted days into spans: the next day joins a span when every day between them is free (no target
// in the schedule, or a weekend without one).
func leaveSpans(cat core.Catalog, emp core.Employer, dates []core.Date) [][2]core.Date {
	var out [][2]core.Date
	for _, date := range dates {
		if n := len(out); n > 0 && freeBetween(cat, emp, out[n-1][1], date) {
			out[n-1][1] = date
			continue
		}
		out = append(out, [2]core.Date{date, date})
	}
	return out
}

func freeBetween(cat core.Catalog, emp core.Employer, from, to core.Date) bool {
	for day := from.AddDays(1); day.Before(to); day = day.AddDays(1) {
		if s, ok := core.ScheduleAt(cat, emp.ID, day); ok {
			if s.Days[day.Weekday()].Minutes > 0 {
				return false
			}
		} else if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
			return false
		}
	}
	return true
}
