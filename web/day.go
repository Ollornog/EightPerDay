// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Ollornog/EightPerDay/core"
)

// The day page books a day's work on projects or clients, with their order numbers: both ways, as the core counts it
// (core.DayTotal). With hours typed for the day the projects split them and the rest is "not booked on projects";
// without, the projects make the day. The weekly report prints a week with its projects.

type dayData struct {
	Employer core.Employer
	Date     core.Date
	Weekday  int
	Day      core.Day
	Typed    int // the hours typed for the day in the list
	Bookings []bookingView
	Projects []core.Project
	Back     string
	List     string
	Locked   bool
}

type bookingView struct {
	core.Entry
	Project string
	Number  string
}

func (h *handler) dayPage(w http.ResponseWriter, r *http.Request) {
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
	date, err := core.ParseDate(r.URL.Query().Get("d"))
	if err != nil {
		date = h.today(emp)
	}
	data, err := core.Load(ctx, h.options.Store, emp.Owner, date, date)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	days, err := core.Days(data, emp.ID, date, date)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	week := date.StartOfWeek(emp.WeekStart)
	d := dayData{Employer: *emp, Date: date, Weekday: int(date.Weekday()), Day: days[0], Projects: projectsOf(cat, emp.ID),
		Back: "/day?e=" + emp.ID + "&d=" + date.String(), List: "/?e=" + emp.ID + "&w=" + week.String() + "#week-" + week.String(),
		Locked: cat.SubmittedLocked(emp.ID, date)}
	for _, e := range data.Entries {
		if e.EmployerID != emp.ID || e.DeletedAt != nil || e.Date != date {
			continue
		}
		if isManualDay(e, emp.ID) {
			d.Typed += e.Minutes
		}
		if e.Kind == core.EntryWork && e.Precision == core.PrecisionDay && e.ProjectID != "" && e.Start == nil {
			p := projectOf(cat, e.ProjectID)
			d.Bookings = append(d.Bookings, bookingView{Entry: e, Project: p.Name, Number: p.Number})
		}
	}
	h.render(w, r, http.StatusOK, "day", view{Title: h.t(r, "day.title"), Data: d})
}

func projectOf(cat core.Catalog, id string) core.Project {
	for _, p := range cat.Projects {
		if p.ID == id {
			return p
		}
	}
	return core.Project{}
}

// bookProjectDay books hours of a day on a project or client, found by name and order number or made new.
func (h *handler) bookProjectDay(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	date, err := formDate(r, "date")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	minutes, err := parseHours(r.PostFormValue("hours"))
	if err != nil || minutes <= 0 || minutes > 24*60 {
		h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "track.error.hours"))
		return
	}
	name, number := strings.TrimSpace(r.PostFormValue("project")), strings.TrimSpace(r.PostFormValue("number"))
	if name == "" {
		h.back(w, r, core.ErrInvalid, "")
		return
	}
	project := ""
	for _, p := range projectsOf(cat, emp.ID) {
		if strings.EqualFold(p.Name, name) && strings.EqualFold(p.Number, number) {
			project = p.ID
		}
	}
	if project == "" {
		p := core.Project{ID: core.NewID(), Owner: emp.Owner, EmployerID: emp.ID, Name: name, Number: number}
		if err := h.options.Store.Put(r.Context(), p); err != nil {
			h.back(w, r, err, "")
			return
		}
		project = p.ID
	}
	loc, _ := emp.Location()
	e := core.Entry{Owner: emp.Owner, EmployerID: emp.ID, Kind: core.EntryWork, Date: date, TimeZone: loc.String(), Minutes: minutes,
		Precision: core.PrecisionDay, ProjectID: project, Source: core.SourceManual, Note: strings.TrimSpace(r.PostFormValue("note"))}
	h.back(w, r, h.book(r, cat, []core.Entry{e}, ""), "saved")
}

// weekReportData is a week as a craftsman hands it in: the days with their projects, the sums and the balance.
type weekReportData struct {
	Employer   core.Employer
	Org        string
	Person     string
	Week       listWeek
	HasAccount bool
	Days       []reportDay
	Back       string
	Printed    string
}

type reportDay struct {
	core.Day
	Weekday  int
	Code     string
	Projects []reportProject
}

type reportProject struct {
	Name, Number string
	Minutes      int
}

func (h *handler) weekReport(w http.ResponseWriter, r *http.Request) {
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
	week := weekOf(r, *emp, today)
	start, hasSchedule := accountStart(cat, emp.ID, week)
	data, err := core.Load(ctx, h.options.Store, emp.Owner, minDate(start, week), week.AddDays(6))
	if err != nil {
		h.failed(w, r, err)
		return
	}
	ledger, err := core.Ledger(data, emp.ID, start, week, week, today)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	weeks, _ := h.listWeeks(r, cat, *emp, data, ledger, today, false, false, func(core.Date) string { return "" })
	d := weekReportData{Employer: *emp, Week: weeks[0], HasAccount: hasSchedule, Person: accountFrom(ctx).DisplayName,
		Back: "/?e=" + emp.ID + "&w=" + week.String(), Printed: formatDate(h.lang(r), toTime(today))}
	if emp.Managed() {
		if org, err := h.options.Store.Organization(ctx, emp.Organization); err == nil {
			d.Org = org.Name
		}
	}
	for _, row := range weeks[0].Rows {
		rd := reportDay{Day: row.Day, Weekday: row.Weekday, Code: row.Code}
		for id, minutes := range row.Day.Projects {
			if id == "" {
				continue
			}
			p := projectOf(cat, id)
			rd.Projects = append(rd.Projects, reportProject{Name: p.Name, Number: p.Number, Minutes: minutes})
		}
		slices.SortFunc(rd.Projects, func(a, b reportProject) int { return strings.Compare(a.Name+a.Number, b.Name+b.Number) })
		d.Days = append(d.Days, rd)
	}
	h.render(w, r, http.StatusOK, "report_week", view{Title: h.t(r, "report.week.title"), Data: d, Print: true})
}
