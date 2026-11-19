// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"log/slog"
	"net/http"

	"github.com/Ollornog/EightPerDay/core"
)

// The list of every week: the newest at the bottom, and the weeks before it load while scrolling up (app.js asks for
// them with part=1, eight at a time). Without JavaScript a link at the top opens the page with more weeks.

// weeksPerLoad is how many weeks the page shows at first and each load adds.
const weeksPerLoad = 8

type weeksData struct {
	Employers  []core.Employer
	Employer   core.Employer
	Weeks      []listWeek
	HasAccount bool
	// Older is the first week of the next load up, when there are weeks before the first one shown.
	HasOlder bool
	Older    core.Date
	First    core.Date
}

// allWeeks shows the list of every week, or with part=1 only the weeks before the one named by before.
func (h *handler) allWeeks(w http.ResponseWriter, r *http.Request) {
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
	q := r.URL.Query()
	to := today.StartOfWeek(emp.WeekStart)
	if before, err := core.ParseDate(q.Get("before")); err == nil {
		to = before.StartOfWeek(emp.WeekStart).AddDays(-7)
	}
	from := to.AddDays(-7 * (weeksPerLoad - 1))
	if want, err := core.ParseDate(q.Get("from")); err == nil && want.Before(from) {
		from = want.StartOfWeek(emp.WeekStart)
	}
	earliest, err := h.earliestWeek(r, cat, *emp, to)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	if from.Before(earliest) {
		from = earliest
	}
	d := weeksData{Employers: activeEmployers(cat), Employer: *emp, First: from, HasOlder: from.After(earliest),
		Older: maxDate(earliest, from.AddDays(-7*weeksPerLoad))}
	if !to.Before(from) {
		start, hasSchedule := accountStart(cat, emp.ID, from)
		d.HasAccount = hasSchedule
		data, err := core.Load(ctx, h.options.Store, emp.Owner, minDate(start, from), to.AddDays(6))
		if err != nil {
			h.failed(w, r, err)
			return
		}
		ledger, err := core.Ledger(data, emp.ID, start, from, to, today)
		if err != nil {
			h.failed(w, r, err)
			return
		}
		_, approval, err := h.approvalOrg(ctx, *emp)
		if err != nil {
			h.failed(w, r, err)
			return
		}
		d.Weeks, _ = h.listWeeks(r, cat, *emp, data, ledger, today, approval, false, func(week core.Date) string {
			return "/weeks?e=" + emp.ID + "&from=" + week.String() + "#week-" + week.String()
		})
	}
	if q.Get("part") == "1" {
		h.renderPart(w, r, "weeks", "weeks-part", d)
		return
	}
	h.render(w, r, http.StatusOK, "weeks", view{Title: h.t(r, "weeks.title"), Data: d, Wide: true})
}

// earliestWeek is the first week with anything of the employer: its first schedule, or its first entry.
func (h *handler) earliestWeek(r *http.Request, cat core.Catalog, emp core.Employer, latest core.Date) (core.Date, error) {
	first := latest
	if start, ok := accountStart(cat, emp.ID, latest); ok && start.Before(first) {
		first = start
	}
	entries, err := h.options.Store.Entries(r.Context(), core.EntryFilter{Owner: emp.Owner, EmployerID: emp.ID})
	if err != nil {
		return first, err
	}
	for _, e := range entries {
		if e.DeletedAt == nil && e.Date.Before(first) {
			first = e.Date
		}
	}
	return first.StartOfWeek(emp.WeekStart), nil
}

// renderPart writes one template of a page alone, for a part the page loads into itself.
func (h *handler) renderPart(w http.ResponseWriter, r *http.Request, page, name string, data any) {
	var out bytes.Buffer
	if err := h.pages[h.lang(r)][page].ExecuteTemplate(&out, name, data); err != nil {
		h.log.Error("template failed", slog.String("page", page), slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out.Bytes())
}

func maxDate(a, b core.Date) core.Date {
	if b.After(a) {
		return b
	}
	return a
}
