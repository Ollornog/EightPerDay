// SPDX-License-Identifier: MIT

package core

import (
	"slices"
	"time"
)

// EarningLine is one line of earnings: a quantity times a rate.
type EarningLine struct {
	RuleID    string   `json:"rule_id"`
	ProjectID string   `json:"project_id,omitempty"`
	Basis     PayBasis `json:"basis"`
	// Quantity is minutes (hourly), days or assignments (per_assignment), 1 (per_project) or the days covered
	// (flat_period).
	Quantity int    `json:"quantity"`
	Rate     int64  `json:"rate"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Earnings are the earnings of one employer in a range.
type Earnings struct {
	EmployerID string           `json:"employer_id"`
	From       Date             `json:"from"`
	To         Date             `json:"to"`
	Lines      []EarningLine    `json:"lines"`
	Total      map[string]int64 `json:"total"`
}

// rulesFor returns the pay rules of the employer with the basis.
func rulesFor(cat Catalog, employerID string, basis PayBasis) []PayRule {
	var out []PayRule
	for _, r := range cat.PayRules {
		if r.EmployerID == employerID && r.Basis == basis {
			out = append(out, r)
		}
	}
	return out
}

// RuleAt returns the rule that applies on the date: a rule of the project before one of the employer, then the later
// valid-from date.
func RuleAt(rules []PayRule, projectID string, d Date) (PayRule, bool) {
	var best PayRule
	found := false
	for _, r := range rules {
		if !d.Within(r.ValidFrom, r.ValidTo) || (r.ProjectID != "" && r.ProjectID != projectID) {
			continue
		}
		if !found || (r.ProjectID != "" && best.ProjectID == "") ||
			((r.ProjectID == "") == (best.ProjectID == "") && !r.ValidFrom.Before(best.ValidFrom)) {
			best, found = r, true
		}
	}
	return best, found
}

// ApplyRate sets the rate snapshot of an entry from the hourly rules valid on its date. It leaves an existing snapshot
// alone; use Revalue to replace it on purpose.
func ApplyRate(cat Catalog, e *Entry) {
	if e.Rate != nil || e.Kind == EntryBreak {
		return
	}
	if r, ok := RuleAt(rulesFor(cat, e.EmployerID, PayHourly), e.ProjectID, e.Date); ok {
		e.Rate = &RateSnapshot{RuleID: r.ID, Amount: r.Amount, Currency: r.Currency}
	}
}

// Revalue returns the entries whose snapshot differs from the rule valid today, with the new snapshot. Storing them
// is the caller's deliberate decision (through UpdateEntry, which keeps the old value in the history).
func Revalue(cat Catalog, entries []Entry) []Entry {
	var out []Entry
	for _, e := range entries {
		if e.Kind == EntryBreak || e.DeletedAt != nil {
			continue
		}
		next := e
		next.Rate = nil
		ApplyRate(cat, &next)
		if !sameRate(e.Rate, next.Rate) {
			out = append(out, next)
		}
	}
	return out
}

func sameRate(a, b *RateSnapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

type lineKey struct {
	rule, project, currency string
	basis                   PayBasis
	rate                    int64
}

// ComputeEarnings calculates what the employer pays from from to to:
//   - hourly: worked minutes times the rate. An entry uses its snapshot, else the rule of its date; days of the mode
//     plan without entries use the employer's rule. Breaks of a day are taken off its work entries in proportion.
//   - per_project: the amount once, when the project is completed in the range.
//   - per_assignment: the amount per worked day (unit day) or per distinct assignment of the entries (unit assignment).
//   - flat_period: the amount per week, month or year, prorated by days for a part of it.
func ComputeEarnings(d Data, employerID string, from, to Date) (Earnings, error) {
	days, err := Days(d, employerID, from, to)
	if err != nil {
		return Earnings{}, err
	}
	out := Earnings{EmployerID: employerID, From: from, To: to, Total: map[string]int64{}}
	lines := map[lineKey]*EarningLine{}
	flat := map[lineKey]int64{} // flat_period amounts, prorated per period
	var order []lineKey
	addLine := func(k lineKey, qty int) {
		l, ok := lines[k]
		if !ok {
			l = &EarningLine{RuleID: k.rule, ProjectID: k.project, Basis: k.basis, Rate: k.rate, Currency: k.currency}
			lines[k] = l
			order = append(order, k)
		}
		l.Quantity += qty
	}

	byDate := map[Date][]Entry{}
	for _, e := range d.Entries {
		if e.EmployerID == employerID && e.DeletedAt == nil && e.Date.Within(from, to) {
			byDate[e.Date] = append(byDate[e.Date], e)
		}
	}
	hourly := rulesFor(d.Catalog, employerID, PayHourly)
	assignment := rulesFor(d.Catalog, employerID, PayPerAssignment)
	seenAssignments := map[string]bool{}

	for _, day := range days {
		entries := byDate[day.Date]
		// Hourly pay.
		type paid struct {
			e       Entry
			minutes int
		}
		var work []paid
		breaks, total := 0, 0
		for _, e := range entries {
			switch e.Kind {
			case EntryWork, EntryAdjust:
				work = append(work, paid{e, e.Minutes})
				if e.Kind == EntryWork {
					total += e.Minutes
				}
			case EntryBreak:
				breaks += e.Minutes
			}
		}
		if day.FromPlan {
			// The plan's minutes (net of deviations, which are entries) at the employer's rule.
			planned := day.Worked
			for _, w := range work {
				planned -= w.minutes
			}
			work = append(work, paid{Entry{EmployerID: employerID, Date: day.Date, Kind: EntryWork}, planned})
		} else if breaks > 0 && total > 0 {
			left := breaks
			for i := range work {
				if work[i].e.Kind != EntryWork || left == 0 {
					continue
				}
				cut := min(left, (breaks*work[i].minutes+total-1)/total, work[i].minutes)
				work[i].minutes -= cut
				left -= cut
			}
		}
		for _, w := range work {
			if w.minutes == 0 {
				continue
			}
			rate := w.e.Rate
			if rate == nil {
				if r, ok := RuleAt(hourly, w.e.ProjectID, day.Date); ok {
					rate = &RateSnapshot{RuleID: r.ID, Amount: r.Amount, Currency: r.Currency}
				}
			}
			if rate != nil {
				addLine(lineKey{rule: rate.RuleID, currency: rate.Currency, basis: PayHourly, rate: rate.Amount}, w.minutes)
			}
		}

		// Per assignment.
		for _, r := range assignment {
			if !day.Date.Within(r.ValidFrom, r.ValidTo) {
				continue
			}
			if r.Unit == UnitAssignment {
				for _, e := range entries {
					if e.Kind == EntryWork && e.Assignment != "" && (r.ProjectID == "" || e.ProjectID == r.ProjectID) &&
						!seenAssignments[r.ID+"\x00"+e.Assignment] {
						seenAssignments[r.ID+"\x00"+e.Assignment] = true
						addLine(lineKey{rule: r.ID, project: r.ProjectID, currency: r.Currency, basis: r.Basis, rate: r.Amount}, 1)
					}
				}
				continue
			}
			worked := day.Worked > 0
			if r.ProjectID != "" {
				worked = day.Projects[r.ProjectID] > 0
			}
			if worked {
				addLine(lineKey{rule: r.ID, project: r.ProjectID, currency: r.Currency, basis: r.Basis, rate: r.Amount}, 1)
			}
		}
	}

	// Per project, on completion.
	for _, r := range rulesFor(d.Catalog, employerID, PayPerProject) {
		for _, p := range d.Catalog.Projects {
			if p.ID == r.ProjectID && !p.CompletedOn.IsZero() && p.CompletedOn.Within(from, to) &&
				p.CompletedOn.Within(r.ValidFrom, r.ValidTo) {
				addLine(lineKey{rule: r.ID, project: r.ProjectID, currency: r.Currency, basis: r.Basis, rate: r.Amount}, 1)
			}
		}
	}

	// Flat per period, prorated by days.
	for _, r := range rulesFor(d.Catalog, employerID, PayFlatPeriod) {
		unit := r.Unit
		if unit == "" {
			unit = UnitMonth
		}
		start, end := from, to
		if start.Before(r.ValidFrom) {
			start = r.ValidFrom
		}
		if !r.ValidTo.IsZero() && end.After(r.ValidTo) {
			end = r.ValidTo
		}
		for p := start; !p.After(end); {
			ps, pe := flatPeriod(p, unit)
			last := pe
			if last.After(end) {
				last = end
			}
			covered := p.DaysUntil(last) + 1
			length := ps.DaysUntil(pe) + 1
			k := lineKey{rule: r.ID, currency: r.Currency, basis: r.Basis, rate: r.Amount}
			addLine(k, covered)
			flat[k] += roundDiv(int64(covered)*r.Amount, int64(length))
			p = pe.AddDays(1)
		}
	}

	for _, k := range order {
		l := lines[k]
		switch l.Basis {
		case PayHourly:
			l.Amount = roundDiv(int64(l.Quantity)*l.Rate, 60)
		case PayFlatPeriod:
			l.Amount = flat[k]
		default:
			l.Amount = int64(l.Quantity) * l.Rate
		}
		out.Lines = append(out.Lines, *l)
		out.Total[l.Currency] += l.Amount
	}
	slices.SortStableFunc(out.Lines, func(a, b EarningLine) int {
		if a.Basis != b.Basis {
			if a.Basis < b.Basis {
				return -1
			}
			return 1
		}
		return 0
	})
	return out, nil
}

// flatPeriod returns the period of the unit that holds d (weeks start on Monday).
func flatPeriod(d Date, unit string) (Date, Date) {
	switch unit {
	case UnitWeek:
		s := d.StartOfWeek(time.Monday)
		return s, s.AddDays(6)
	case UnitYear:
		return NewDate(d.Year, time.January, 1), NewDate(d.Year, time.December, 31)
	}
	s := d.StartOfMonth()
	return s, s.AddMonths(1).AddDays(-1)
}

// roundDiv divides and rounds half away from zero.
func roundDiv(n, d int64) int64 {
	if n < 0 {
		return -((-n + d/2) / d)
	}
	return (n + d/2) / d
}
