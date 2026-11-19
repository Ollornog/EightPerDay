// SPDX-License-Identifier: MIT

package web

import (
	"net/http"

	"github.com/Ollornog/EightPerDay/core"
)

// The week of every employer in one list, the way people with several jobs in a season keep it: a column per employer
// that is active that week, a cell only on the days it is, and the earnings per day, week and the average working
// day, with what was taken in cash or by card.

type allData struct {
	Week, Prev, Next core.Date
	Number           int
	Columns          []allColumn
	Rows             []allRow
	Currency         string
	Earned           int64
	Average          int64
	WorkDays         int
	Cash, Card       int64
	Back             string
}

type allColumn struct {
	Employer core.Employer
	Minutes  int
	Earned   int64
}

type allRow struct {
	Date    core.Date
	Weekday int
	IsToday bool
	Cells   []allCell
	Earned  int64
	Worked  bool
	Place   string
	Km      string
}

type allCell struct {
	Active bool
	Hours  string
	Code   string
	Earned int64
}

func (h *handler) allEmployers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat, err := h.ownCatalog(ctx)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	employers := activeEmployers(cat)
	if len(employers) == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	today := h.today(&employers[0])
	week := weekOf(r, employers[0], today)
	end := week.AddDays(6)
	_, number := toTime(week).ISOWeek()
	d := allData{Week: week, Prev: week.AddDays(-7), Next: week.AddDays(7), Number: number,
		Currency: employerCurrency(cat, employers[0].ID), Back: "/all?w=" + week.String()}
	from := week
	for _, e := range employers {
		from = minDate(from, core.EarningsDataFrom(cat, e.ID, week))
	}
	data, err := core.Load(ctx, h.options.Store, accountFrom(ctx).ID, from, end)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	lang := h.lang(r)
	for i := range 7 {
		day := week.AddDays(i)
		d.Rows = append(d.Rows, allRow{Date: day, Weekday: int(day.Weekday()), IsToday: day == today})
	}
	for _, info := range cat.DayInfos {
		for i := range d.Rows {
			if info.Date == d.Rows[i].Date && info.Place != "" && d.Rows[i].Place == "" {
				d.Rows[i].Place = info.Place
			}
			if info.Date == d.Rows[i].Date && info.KmTotal > 0 {
				d.Rows[i].Km = kmText(lang, info.KmTotal)
			}
		}
	}
	for _, emp := range employers {
		days, err := core.Days(data, emp.ID, week, end)
		if err != nil {
			h.failed(w, r, err)
			return
		}
		codes := h.dayCodes(r, cat, emp, data)
		col := allColumn{Employer: emp}
		cells := make([]allCell, 7)
		active := false
		for i, day := range days {
			earnings, err := core.ComputeEarnings(data, emp.ID, day.Date, day.Date)
			if err != nil {
				h.failed(w, r, err)
				return
			}
			c := allCell{Code: codes[day.Date].text, Earned: earnings.Total[d.Currency]}
			if day.Worked > 0 {
				c.Hours = hoursText(lang, day.Worked)
			}
			c.Active = day.Worked > 0 || c.Code != "" || hasIncome(cat, emp.ID, day.Date)
			if c.Active {
				active = true
				col.Minutes += day.Worked
			}
			col.Earned += c.Earned
			cells[i] = c
			d.Rows[i].Earned += c.Earned
			d.Rows[i].Worked = d.Rows[i].Worked || day.Worked > 0
			d.Cash += earnings.Taken[core.PaidCash][d.Currency]
			d.Card += earnings.Taken[core.PaidCard][d.Currency]
		}
		if !active {
			continue
		}
		d.Columns = append(d.Columns, col)
		for i := range d.Rows {
			d.Rows[i].Cells = append(d.Rows[i].Cells, cells[i])
		}
		d.Earned += col.Earned
	}
	for _, row := range d.Rows {
		if row.Worked {
			d.WorkDays++
		}
	}
	if d.WorkDays > 0 {
		d.Average = (d.Earned + int64(d.WorkDays)/2) / int64(d.WorkDays)
	}
	h.render(w, r, http.StatusOK, "all", view{Title: h.t(r, "all.title"), Data: d, Wide: true})
}

func hasIncome(cat core.Catalog, employerID string, d core.Date) bool {
	for _, i := range cat.Incomes {
		if i.EmployerID == employerID && i.Date == d {
			return true
		}
	}
	return false
}
