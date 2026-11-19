// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/web/auth"
)

// The forms that book time. Each one names its employer; the person books only on their own (core.MayBook), and the
// store keeps the history of every change.

// bookingContext reads the catalog and the employer a form names.
func (h *handler) bookingContext(w http.ResponseWriter, r *http.Request) (core.Catalog, core.Employer, bool) {
	cat, err := h.ownCatalog(r.Context())
	if err != nil {
		h.failed(w, r, err)
		return cat, core.Employer{}, false
	}
	emp, err := h.formEmployer(r, cat)
	if err != nil {
		h.back(w, r, err, "")
		return cat, emp, false
	}
	return cat, emp, true
}

// replaceDayValue keeps one manual value per day (or week, or project): the entry of the form is updated, removed when
// the value is cleared, or made when there is none.
func (h *handler) replaceDayValue(r *http.Request, cat core.Catalog, emp core.Employer, date core.Date, precision core.Precision,
	projectID string, minutes int) error {
	ctx := r.Context()
	owner := accountFrom(ctx).ID
	entries, err := h.options.Store.Entries(ctx, core.EntryFilter{Owner: owner, EmployerID: emp.ID, From: date, To: date})
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.DeletedAt != nil || e.Kind != core.EntryWork || e.Precision != precision || e.Start != nil || e.ProjectID != projectID ||
			e.Source != core.SourceManual {
			continue
		}
		if minutes == 0 {
			return h.options.Store.DeleteEntry(ctx, owner, e.ID, e.Version, change(r, ""))
		}
		if e.Minutes == minutes {
			return nil
		}
		e.Minutes = minutes
		_, err := h.options.Store.UpdateEntry(ctx, e, change(r, ""))
		return err
	}
	if minutes == 0 {
		return nil
	}
	loc, _ := emp.Location()
	return h.book(r, cat, []core.Entry{{Owner: owner, EmployerID: emp.ID, Kind: core.EntryWork, Date: date, TimeZone: loc.String(),
		Minutes: minutes, Precision: precision, ProjectID: projectID, Source: core.SourceManual}}, "")
}

// weekTotal books the one number of the mode week_sum.
func (h *handler) weekTotal(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	week, err := formDate(r, "week")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	minutes, err := parseHours(r.PostFormValue("hours"))
	if err != nil || minutes < 0 {
		h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "track.error.hours"))
		return
	}
	h.back(w, r, h.replaceDayValue(r, cat, emp, week.StartOfWeek(emp.WeekStart), core.PrecisionWeek, "", minutes), "saved")
}

// dayTotals books the hours per day of the mode schedule, one field per day.
func (h *handler) dayTotals(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.back(w, r, err, "")
		return
	}
	for key, values := range r.PostForm {
		raw, found := strings.CutPrefix(key, "day-")
		if !found || len(values) != 1 {
			continue
		}
		date, err := core.ParseDate(raw)
		if err != nil {
			continue
		}
		minutes, err := parseHours(values[0])
		if err != nil || minutes < 0 {
			h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "track.error.hours"))
			return
		}
		if err := h.replaceDayValue(r, cat, emp, date, core.PrecisionDay, "", minutes); err != nil {
			h.back(w, r, err, "")
			return
		}
	}
	h.back(w, r, nil, "saved")
}

// projectTotals books the hours per project of a week, for the mode projects_only.
func (h *handler) projectTotals(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	week, err := formDate(r, "week")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.back(w, r, err, "")
		return
	}
	for _, p := range projectsOf(cat, emp.ID) {
		raw, present := r.PostForm["project-"+p.ID]
		if !present || len(raw) != 1 {
			continue
		}
		minutes, err := parseHours(raw[0])
		if err != nil || minutes < 0 {
			h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "track.error.hours"))
			return
		}
		if err := h.replaceDayValue(r, cat, emp, week.StartOfWeek(emp.WeekStart), core.PrecisionWeek, p.ID, minutes); err != nil {
			h.back(w, r, err, "")
			return
		}
	}
	h.back(w, r, nil, "saved")
}

// confirmWeek is "the week is right" of the mode plan: the plan days up to today count as declared.
func (h *handler) confirmWeek(w http.ResponseWriter, r *http.Request) {
	_, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	week, err := formDate(r, "week")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	from := week.StartOfWeek(emp.WeekStart)
	to := minDate(from.AddDays(6), h.today(&emp))
	c := core.Confirmation{ID: core.NewID(), Owner: emp.Owner, EmployerID: emp.ID, From: from, To: to, ConfirmedAt: h.now()}
	h.back(w, r, h.options.Store.Put(r.Context(), c), "confirmed")
}

// adjust books a deviation from the plan: longer (+) or left early (-).
func (h *handler) adjust(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	date, err := formDate(r, "date")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	minutes, err := parseHours(r.PostFormValue("minutes"))
	if err != nil || minutes == 0 {
		h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "track.error.hours"))
		return
	}
	if r.PostFormValue("direction") == "less" && minutes > 0 {
		minutes = -minutes
	}
	loc, _ := emp.Location()
	e := core.Entry{Owner: emp.Owner, EmployerID: emp.ID, Kind: core.EntryAdjust, Date: date, TimeZone: loc.String(),
		Minutes: minutes, Precision: core.PrecisionDay, Note: strings.TrimSpace(r.PostFormValue("note")), Source: core.SourceManual}
	h.back(w, r, h.book(r, cat, []core.Entry{e}, ""), "saved")
}

// addAbsence books leave, sickness or another absence for the employer of the form.
func (h *handler) addAbsence(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	from, err := formDate(r, "from")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	to := from
	if raw := strings.TrimSpace(r.PostFormValue("to")); raw != "" {
		if to, err = core.ParseDate(raw); err != nil {
			h.back(w, r, err, "")
			return
		}
	}
	a := core.Absence{Owner: emp.Owner, EmployerID: emp.ID, Type: r.PostFormValue("type"), From: from, To: to,
		HalfDay: r.PostFormValue("half") == "on", Note: strings.TrimSpace(r.PostFormValue("note"))}
	h.back(w, r, h.createAbsence(r, cat, emp, a), "saved")
}

// removeAbsence marks an absence as deleted; the history keeps it.
func (h *handler) removeAbsence(w http.ResponseWriter, r *http.Request) {
	owner := accountFrom(r.Context()).ID
	version, _ := strconv.Atoi(r.PostFormValue("version"))
	h.back(w, r, h.options.Store.DeleteAbsence(r.Context(), owner, r.PathValue("id"), version, change(r, r.PostFormValue("reason"))), "removed")
}

// addSpan books a span with start and end, and a break, for the mode exact.
func (h *handler) addSpan(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	date, err := formDate(r, "date")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	start, okStart := parseClock(r.PostFormValue("start"))
	end, okEnd := parseClock(r.PostFormValue("end"))
	if !okStart || !okEnd {
		h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "track.error.clock"))
		return
	}
	if end <= start {
		end += 24 * 60 // past midnight
	}
	pause, err := parseHours(r.PostFormValue("break"))
	if err != nil || pause < 0 {
		h.message(w, r, http.StatusBadRequest, h.t(r, "track.error.title"), h.t(r, "track.error.hours"))
		return
	}
	parsed := core.Parsed{Date: date, Start: &start, End: &end, Break: pause, Note: strings.TrimSpace(r.PostFormValue("note"))}
	h.bookParsed(w, r, cat, emp, parsed, r.PostFormValue("project"), core.SourceManual)
}

// bookParsed books what a line or a form describes: entries, or an absence.
func (h *handler) bookParsed(w http.ResponseWriter, r *http.Request, cat core.Catalog, emp core.Employer, p core.Parsed, projectID string,
	source core.Source) {
	today := h.today(&emp)
	if p.Absence != "" {
		a, err := p.AbsenceRecord(emp.Owner, emp.ID, today)
		if err == nil {
			err = h.createAbsence(r, cat, emp, a)
		}
		h.back(w, r, err, "saved")
		return
	}
	if p.Project != "" {
		id, err := h.projectFor(r, cat, emp, p.Project)
		if err != nil {
			h.back(w, r, err, "")
			return
		}
		projectID = id
	}
	if projectID != "" && projectName(cat, projectID) == "" && p.Project == "" {
		h.back(w, r, core.ErrNotFound, "")
		return
	}
	loc, err := emp.Location()
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	entries, err := p.Entries(core.EntryOptions{Owner: emp.Owner, EmployerID: emp.ID, ProjectID: projectID, Location: loc, Today: today})
	if err == nil && len(entries) == 0 {
		err = core.ErrInvalid
	}
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	for i := range entries {
		entries[i].Source = source
	}
	h.back(w, r, h.book(r, cat, entries, ""), "saved")
}

// freeText books a line such as "8-16:30 30m Pause", "Fr -2h früher weg" or "Mo-Mi Urlaub".
func (h *handler) freeText(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	parsed, err := core.Parse(r.PostFormValue("line"), h.today(&emp))
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	h.bookParsed(w, r, cat, emp, parsed, "", core.SourceParse)
}

// parsePreview says what a line would book, while it is typed. It only reads.
func (h *handler) parsePreview(w http.ResponseWriter, r *http.Request) {
	parsed, err := core.Parse(r.URL.Query().Get("line"), h.today(nil))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": h.storeError(r, err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": h.describe(r, parsed)})
}

// describe puts what the parser read into words.
func (h *handler) describe(r *http.Request, p core.Parsed) string {
	var parts []string
	if !p.Date.IsZero() {
		day := h.t(r, "weekday."+strconv.Itoa(int(p.Date.Weekday()))) + " " + formatDate(h.lang(r), toTime(p.Date))
		if !p.To.IsZero() && p.To != p.Date {
			day += " – " + formatDate(h.lang(r), toTime(p.To))
		}
		parts = append(parts, day)
	}
	switch {
	case p.Absence != "":
		name := p.Absence
		for _, t := range core.DefaultAbsenceTypes() {
			if t.Key == p.Absence {
				name = h.t(r, "absence."+t.Key)
			}
		}
		if p.HalfDay {
			name += " (" + h.t(r, "track.half") + ")"
		}
		parts = append(parts, name)
	case p.Start != nil && p.End != nil:
		parts = append(parts, clockText(*p.Start)+"–"+clockText(*p.End%(24*60)))
	case p.Adjust:
		sign := "+"
		if p.Minutes < 0 {
			sign = ""
		}
		parts = append(parts, h.t(r, "track.deviation")+" "+sign+formatMinutes(p.Minutes))
	case p.Minutes > 0:
		parts = append(parts, formatMinutes(p.Minutes)+" h")
	}
	if p.Break > 0 {
		parts = append(parts, h.t(r, "track.break")+" "+formatMinutes(p.Break))
	}
	if p.Project != "" {
		parts = append(parts, "@"+p.Project)
	}
	for _, t := range p.Tags {
		parts = append(parts, "#"+t)
	}
	if p.Note != "" {
		parts = append(parts, "„"+p.Note+"“")
	}
	if len(parts) == 0 {
		return h.t(r, "track.nothing")
	}
	return strings.Join(parts, " · ")
}

func clockText(minutes int) string {
	return strconv.Itoa(minutes/60) + ":" + leftPad(minutes%60)
}

func leftPad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func toTime(d core.Date) time.Time { return time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC) }

func (h *handler) lang(r *http.Request) string {
	accountLanguage := ""
	if a := accountFrom(r.Context()); a != nil {
		accountLanguage = a.Language
	}
	return pickLanguage(r, accountLanguage)
}

// removeEntry marks an entry as deleted, with the reason in the history.
func (h *handler) removeEntry(w http.ResponseWriter, r *http.Request) {
	owner := accountFrom(r.Context()).ID
	version, _ := strconv.Atoi(r.PostFormValue("version"))
	h.back(w, r, h.options.Store.DeleteEntry(r.Context(), owner, r.PathValue("id"), version, change(r, r.PostFormValue("reason"))), "removed")
}

// startTimer starts the stopwatch on the server, so it runs on while the phone sleeps.
func (h *handler) startTimer(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	projectID := r.PostFormValue("project")
	if projectID != "" && projectName(cat, projectID) == "" {
		h.back(w, r, core.ErrNotFound, "")
		return
	}
	err := h.options.Timers.StartTimer(r.Context(), accountFrom(r.Context()).ID, auth.Timer{EmployerID: emp.ID, ProjectID: projectID,
		Note: strings.TrimSpace(r.PostFormValue("note")), Started: h.now().Truncate(time.Minute)})
	h.back(w, r, err, "started")
}

// stopTimer books the span of the stopwatch and stops it. A span under a minute is dropped.
func (h *handler) stopTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account := accountFrom(ctx)
	timer, err := h.options.Timers.RunningTimer(ctx, account.ID)
	if err != nil || timer == nil {
		h.back(w, r, err, "stopped")
		return
	}
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	emp, found := cat.Employer(timer.EmployerID)
	end := h.now().Truncate(time.Minute)
	if found && end.After(timer.Started) {
		loc, _ := emp.Location()
		start := timer.Started
		e := core.Entry{Owner: account.ID, EmployerID: emp.ID, Kind: core.EntryWork, Date: core.DateIn(start, loc), TimeZone: loc.String(),
			Start: &start, End: &end, Precision: core.PrecisionSpan, ProjectID: timer.ProjectID, Note: timer.Note, Source: core.SourceTimer}
		if err := e.Normalize(); err != nil {
			h.back(w, r, err, "")
			return
		}
		// Booked first, stopped after: a failure keeps the clock running instead of losing the time.
		if err := h.book(r, cat, []core.Entry{e}, ""); err != nil {
			h.back(w, r, err, "")
			return
		}
	}
	_, err = h.options.Timers.StopTimer(ctx, account.ID)
	h.back(w, r, err, "stopped")
}

// spreadWeek splits the weekly total of the mode week_sum into hours per day (core.SpreadWeek, by the schedule's
// targets) and switches the employer to the mode schedule from that week on, for "split this week into days". The sum
// stays the same; the week entry goes through the history.
func (h *handler) spreadWeek(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	if !core.MayChangeSettings(emp, core.Actor{Person: accountFrom(r.Context()).ID}) {
		h.back(w, r, errForbidden, "")
		return
	}
	week, err := formDate(r, "week")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	from := week.StartOfWeek(emp.WeekStart)
	ctx := r.Context()
	owner := accountFrom(ctx).ID
	entries, err := h.options.Store.Entries(ctx, core.EntryFilter{Owner: owner, EmployerID: emp.ID, From: from, To: from})
	if err != nil {
		h.failed(w, r, err)
		return
	}
	spread := 0
	for _, e := range entries {
		if e.DeletedAt != nil || e.Kind != core.EntryWork || e.Precision != core.PrecisionWeek || e.ProjectID != "" {
			continue
		}
		parts, err := core.SpreadWeek(cat, e)
		if err != nil {
			h.back(w, r, err, "")
			return
		}
		// The parts first, the week entry after: a failure in between counts the time twice, never not at all.
		if err := h.book(r, cat, parts, "week to days"); err != nil {
			h.back(w, r, err, "")
			return
		}
		if err := h.options.Store.DeleteEntry(ctx, owner, e.ID, e.Version, change(r, "week to days")); err != nil {
			h.back(w, r, err, "")
			return
		}
		spread++
	}
	if spread == 0 {
		h.back(w, r, core.ErrNotFound, "")
		return
	}
	mc := core.ModeChange{ID: core.NewID(), Owner: emp.Owner, EmployerID: emp.ID, ValidFrom: from, Mode: core.ModeSchedule}
	h.back(w, r, h.options.Store.Put(ctx, mc), "spread")
}

// materializeWeek turns the plan days of a week into entries and switches the employer to exact from that week on,
// for "record this week exactly". The minutes per day stay the same.
func (h *handler) materializeWeek(w http.ResponseWriter, r *http.Request) {
	cat, emp, ok := h.bookingContext(w, r)
	if !ok {
		return
	}
	if !core.MayChangeSettings(emp, core.Actor{Person: accountFrom(r.Context()).ID}) {
		h.back(w, r, errForbidden, "")
		return
	}
	week, err := formDate(r, "week")
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	from := week.StartOfWeek(emp.WeekStart)
	data, err := core.Load(r.Context(), h.options.Store, emp.Owner, from, from.AddDays(6))
	if err != nil {
		h.failed(w, r, err)
		return
	}
	entries, err := core.MaterializePlan(data, emp.ID, from, minDate(from.AddDays(6), h.today(&emp)))
	if err != nil {
		h.back(w, r, err, "")
		return
	}
	if err := h.book(r, cat, entries, "plan to exact"); err != nil {
		h.back(w, r, err, "")
		return
	}
	mc := core.ModeChange{ID: core.NewID(), Owner: emp.Owner, EmployerID: emp.ID, ValidFrom: from, Mode: core.ModeExact}
	h.back(w, r, h.options.Store.Put(r.Context(), mc), "exact")
}
