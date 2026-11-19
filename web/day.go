// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
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
	// Incomes of the day (a job with its client, commission, photos, tips) and the details of the day.
	Incomes     []core.Income
	IncomeKinds []core.IncomeKind
	Info        core.DayInfo
	KmOneWay    string
	KmTotal     string
	Currency    string
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
	d.IncomeKinds, d.Currency = core.IncomeKinds, employerCurrency(cat, emp.ID)
	for _, i := range cat.Incomes {
		if i.EmployerID == emp.ID && i.Date == date {
			d.Incomes = append(d.Incomes, i)
		}
	}
	for _, i := range cat.DayInfos {
		if i.ID == core.DayInfoID(emp.ID, date) {
			d.Info = i
		}
	}
	d.KmOneWay, d.KmTotal = kmText(h.lang(r), d.Info.KmOneWay), kmText(h.lang(r), d.Info.KmTotal)
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

// employerCurrency is the currency of the employer's latest pay rule, or euros.
func employerCurrency(cat core.Catalog, employerID string) string {
	currency, since := "EUR", core.Date{}
	for _, p := range cat.PayRules {
		if p.EmployerID == employerID && !p.ValidFrom.Before(since) {
			currency, since = p.Currency, p.ValidFrom
		}
	}
	return currency
}

// kmText writes tenths of kilometres as typed: 12,5 or 40.
func kmText(lang string, tenths int) string {
	if tenths == 0 {
		return ""
	}
	text := strconv.Itoa(tenths / 10)
	if tenths%10 != 0 {
		text += "." + strconv.Itoa(tenths%10)
	}
	if lang == "de" {
		text = strings.ReplaceAll(text, ".", ",")
	}
	return text
}

// parseKm reads kilometres such as 12,5 or 40 as tenths; empty is zero.
func parseKm(input string) (int, error) {
	s := strings.ReplaceAll(strings.TrimSpace(input), ",", ".")
	if s == "" {
		return 0, nil
	}
	km, err := strconv.ParseFloat(s, 64)
	if err != nil || km < 0 || km > 10000 {
		return 0, core.ErrInvalid
	}
	return int(km*10 + 0.5), nil
}

// bookIncome books money earned on a day: a job with its client, a commission, photos, a tip, and how it was taken.
func (h *handler) bookIncome(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	date, err := formDate(r, "date")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	amount, err := parseMoney(r.PostFormValue("amount"))
	if err != nil {
		h.back(w, r, core.ErrInvalid, "")
		return
	}
	i := core.Income{ID: core.NewID(), Owner: emp.Owner, EmployerID: emp.ID, Date: date, Kind: core.IncomeKind(r.PostFormValue("kind")),
		Client: strings.TrimSpace(r.PostFormValue("client")), Amount: amount, Currency: employerCurrency(cat, emp.ID),
		PaidBy: core.PaidBy(r.PostFormValue("paid_by")), Note: strings.TrimSpace(r.PostFormValue("note"))}
	h.back(w, r, h.options.Store.Put(r.Context(), i), "saved")
}

// removeIncome takes an income back.
func (h *handler) removeIncome(w http.ResponseWriter, r *http.Request) {
	cat, err := h.ownCatalog(r.Context())
	if err != nil {
		h.failed(w, r, err)
		return
	}
	for _, i := range cat.Incomes {
		if i.ID != r.PathValue("id") {
			continue
		}
		emp, ok := cat.Employer(i.EmployerID)
		if !ok || !core.MayBook(emp, core.Actor{Person: accountFrom(r.Context()).ID}) {
			h.back(w, r, errForbidden, "")
			return
		}
		h.back(w, r, h.options.Store.Delete(r.Context(), core.RecIncome, i.Owner, i.ID), "removed")
		return
	}
	h.back(w, r, core.ErrNotFound, "")
}

// bookDayInfo keeps the details of a day: place of work, kilometres, standby. All empty removes them.
func (h *handler) bookDayInfo(w http.ResponseWriter, r *http.Request) {
	_, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	date, err := formDate(r, "date")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	i := core.DayInfo{ID: core.DayInfoID(emp.ID, date), Owner: emp.Owner, EmployerID: emp.ID, Date: date,
		Place: strings.TrimSpace(r.PostFormValue("place"))}
	var errs [4]error
	i.KmOneWay, errs[0] = parseKm(r.PostFormValue("km_one_way"))
	i.KmTotal, errs[1] = parseKm(r.PostFormValue("km_total"))
	i.StandbyPassive, errs[2] = parseHours(r.PostFormValue("standby_passive"))
	i.StandbyActive, errs[3] = parseHours(r.PostFormValue("standby_active"))
	for _, err := range errs {
		if err != nil {
			h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "day.error.details"))
			return
		}
	}
	if i == (core.DayInfo{ID: i.ID, Owner: i.Owner, EmployerID: i.EmployerID, Date: date}) {
		err := h.options.Store.Delete(r.Context(), core.RecDayInfo, i.Owner, i.ID)
		if errors.Is(err, core.ErrNotFound) {
			err = nil
		}
		h.back(w, r, err, "saved")
		return
	}
	h.back(w, r, h.options.Store.Put(r.Context(), i), "saved")
}
