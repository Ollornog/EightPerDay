// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ollornog/EightPerDay/core"
)

// reportData is the report of a period: per employer the time, the accounts and the earnings, and the person's own
// view over all employers with the hints on the Working Hours Act, which only the person sees.
type reportData struct {
	From, To  core.Date
	Prev      core.Date
	Next      core.Date
	Employers []employerReport
	Overview  core.Overview
	Hints     []core.Hint
	MaxWeek   int
}

type employerReport struct {
	Employer  core.Employer
	Mode      core.Mode
	Total     core.Total
	HasTarget bool
	Account   core.OvertimeAccount
	Leave     core.LeaveAccount
	HasLeave  bool
	Earnings  []earningView
	Projects  []projectRow
}

type earningView struct {
	core.EarningLine
	Project string
	Amount  string
	Rate    string
	// Label says what the line pays for: the basis, and for a paid absence or a surcharge its reason.
	Label string
}

type moneyTotal struct {
	Currency string
	Cents    int64
}

func (h *handler) reports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account := accountFrom(ctx)
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	today := h.today(nil)
	from := today.StartOfMonth()
	if raw := r.URL.Query().Get("month"); raw != "" {
		if d, err := core.ParseDate(raw + "-01"); err == nil {
			from = d
		}
	}
	to := from.AddMonths(1).AddDays(-1)
	d := reportData{From: from, To: to, Prev: from.AddMonths(-1), Next: from.AddMonths(1)}
	data, err := core.Load(ctx, h.options.Store, account.ID, core.NewDate(from.Year, time.January, 1), core.NewDate(from.Year, time.December, 31))
	if err != nil {
		h.failed(w, r, err)
		return
	}
	lang := h.lang(r)
	for _, emp := range activeEmployers(cat) {
		days, err := core.Days(data, emp.ID, from, to)
		if err != nil {
			h.failed(w, r, err)
			return
		}
		e := employerReport{Employer: emp, Mode: core.ModeAt(cat, emp.ID, to), Total: core.Sum(days)}
		for _, day := range days {
			if day.HasTarget {
				e.HasTarget = true
			}
		}
		if _, ok := core.ScheduleAt(cat, emp.ID, to); ok {
			if e.Account, err = core.Overtime(data, emp.ID, core.NewDate(from.Year, time.January, 1), minDate(to, today)); err != nil {
				h.failed(w, r, err)
				return
			}
			if e.Leave, err = core.Leave(data, emp.ID, from.Year); err != nil {
				h.failed(w, r, err)
				return
			}
			e.HasLeave = e.Leave.Entitlement > 0 || e.Leave.Taken > 0
		}
		earnings, err := core.ComputeEarnings(data, emp.ID, from, to)
		if err != nil {
			h.failed(w, r, err)
			return
		}
		for _, line := range earnings.Lines {
			e.Earnings = append(e.Earnings, earningView{EarningLine: line, Project: projectName(cat, line.ProjectID),
				Amount: formatMoney(lang, line.Amount, line.Currency), Rate: formatMoney(lang, line.Rate, line.Currency),
				Label: h.earningLabel(r, cat, emp.ID, line)})
		}
		for _, p := range projectsOf(cat, emp.ID) {
			if minutes := e.Total.Projects[p.ID]; minutes > 0 {
				e.Projects = append(e.Projects, projectRow{Project: p, Minutes: minutes})
			}
		}
		d.Employers = append(d.Employers, e)
	}
	if d.Overview, err = core.PersonalOverview(data, from, to, time.Monday); err != nil {
		h.failed(w, r, err)
		return
	}
	d.Hints = d.Overview.Hints
	for _, week := range d.Overview.Weeks {
		d.MaxWeek = max(d.MaxWeek, week.Worked)
	}
	h.render(w, r, http.StatusOK, "reports", view{Title: h.t(r, "reports.title"), Data: d})
}

// earningLabel names a line of the earnings: "Hourly", "Paid absence: Leave", "Surcharge night 25 %", "Per assignment,
// half days", "Standby, active / job".
func (h *handler) earningLabel(r *http.Request, cat core.Catalog, employerID string, line core.EarningLine) string {
	label := h.t(r, "pay."+string(line.Basis))
	switch line.Basis {
	case core.PayAbsence:
		name := h.t(r, "absence.public_holiday")
		if line.Reason != core.PublicHolidayPay {
			name = line.Reason
			for _, t := range cat.AbsenceTypesFor(employerID) {
				if t.Key == line.Reason {
					name = h.absenceName(r, t)
				}
			}
		}
		label += ": " + name
	case core.PaySurcharge:
		label += ": " + h.t(r, "surcharge."+line.Reason) + " " + formatPercent(h.lang(r), line.BasisPoints)
	case core.PayStandbyPassive, core.PayStandbyActive:
		unit := "hour"
		if line.Unit != "" {
			unit = line.Unit
		}
		label += " / " + h.t(r, "unit."+unit)
	}
	if line.HalfDay {
		label += ", " + h.t(r, "earning.half_day")
	}
	return label
}

// formatPercent writes basis points as a percentage: 2500 is "25 %", 1250 "12,5 %" in German.
func formatPercent(lang string, basisPoints int) string {
	whole, rest := strconv.Itoa(basisPoints/100), basisPoints%100
	if rest != 0 {
		separator := "."
		if lang == "de" {
			separator = ","
		}
		whole += separator + strings.TrimRight(leftPad(rest), "0")
	}
	return whole + " %"
}

// totals sums the earnings of a report per currency, for the line at the bottom.
func (e employerReport) Totals() []moneyTotal {
	sums := map[string]int64{}
	for _, line := range e.Earnings {
		sums[line.Currency] += line.EarningLine.Amount
	}
	var out []moneyTotal
	for currency, amount := range sums {
		out = append(out, moneyTotal{Currency: currency, Cents: amount})
	}
	slices.SortFunc(out, func(a, b moneyTotal) int {
		if a.Currency < b.Currency {
			return -1
		}
		return 1
	})
	return out
}

// export hands the person everything about them: JSON with the whole record, or the entries as CSV (Art. 15, 20 GDPR).
func (h *handler) export(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account := accountFrom(ctx)
	x, err := core.ExportOwner(ctx, h.options.Store, account.ID)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	var out bytes.Buffer
	name := "eightperday-" + h.today(nil).String()
	switch r.PathValue("format") {
	case "json":
		err = x.WriteJSON(&out)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		name += ".json"
	case "csv":
		err = core.WriteEntriesCSV(&out, x.Entries)
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		name += ".csv"
	default:
		h.notFound(w, r)
		return
	}
	if err != nil {
		h.failed(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out.Bytes())
}
