// SPDX-License-Identifier: MIT

package core

import (
	"math"
	"slices"
	"strings"
	"time"
)

// EarningLine is one line of earnings: a quantity times a rate.
type EarningLine struct {
	RuleID    string   `json:"rule_id"`
	ProjectID string   `json:"project_id,omitempty"`
	Basis     PayBasis `json:"basis"`
	// Reason says what the line pays for: ReasonWork, the absence type (or PublicHolidayPay) of a paid absence, or the
	// SurchargeKind of a surcharge.
	Reason string `json:"reason"`
	// Quantity is minutes (hourly, paid absences, surcharges), days or assignments (per_assignment), 1 (per_project)
	// or the days covered (flat_period).
	Quantity int `json:"quantity"`
	// Rate is per hour for minutes (for an average the average per hour, rounded), else per unit.
	Rate int64 `json:"rate"`
	// BasisPoints is the percentage of a surcharge (2500 is 25 %).
	BasisPoints int    `json:"basis_points,omitempty"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	// HalfDay: the line pays half days (per_assignment per day, Day.Part) at the rule's amount for half a day.
	HalfDay bool `json:"half_day,omitempty"`
	// Unit is the unit of a standby line: empty for minutes at a rate per hour, "day" or "assignment" (call-outs).
	Unit string `json:"unit,omitempty"`
}

// ReasonTotal sums the lines of one basis and reason in one currency.
type ReasonTotal struct {
	Basis    PayBasis `json:"basis"`
	Reason   string   `json:"reason"`
	Currency string   `json:"currency"`
	Amount   int64    `json:"amount"`
}

// Earnings are the earnings of one employer in a range.
type Earnings struct {
	EmployerID string           `json:"employer_id"`
	From       Date             `json:"from"`
	To         Date             `json:"to"`
	Lines      []EarningLine    `json:"lines"`
	Total      map[string]int64 `json:"total"`
	// Reasons splits the total by basis and reason: work, each paid absence, each surcharge, each kind of income.
	Reasons []ReasonTotal `json:"reasons"`
	// Taken sums the incomes by how they were taken (cash, card, transfer) and currency.
	Taken map[PaidBy]map[string]int64 `json:"taken,omitempty"`
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
	rule, project, currency, reason string
	basis                           PayBasis
	rate                            int64
	basisPoints                     int
	half                            bool
	unit                            string
	// direct: the amount is summed as it goes (flat periods, averages), not rate times quantity at the end.
	direct bool
	ref    Date
}

// earningsBuilder collects the lines of ComputeEarnings.
type earningsBuilder struct {
	d        Data
	v        *employerView
	from, to Date
	days     []Day
	byDate   map[Date][]Entry
	hourly   []PayRule
	lines    map[lineKey]*EarningLine
	direct   map[lineKey]int64
	order    []lineKey
	averages map[averageKey]average
}

func newEarningsBuilder(d Data, employerID string, from, to Date) (*earningsBuilder, error) {
	days, err := Days(d, employerID, from, to)
	if err != nil {
		return nil, err
	}
	v, err := newEmployerView(d, employerID)
	if err != nil {
		return nil, err
	}
	return &earningsBuilder{d: d, v: v, from: from, to: to, days: days, byDate: v.entries,
		hourly: rulesFor(d.Catalog, employerID, PayHourly), lines: map[lineKey]*EarningLine{},
		direct: map[lineKey]int64{}, averages: map[averageKey]average{}}, nil
}

func (b *earningsBuilder) add(k lineKey, qty int, amount int64) {
	l, ok := b.lines[k]
	if !ok {
		l = &EarningLine{RuleID: k.rule, ProjectID: k.project, Basis: k.basis, Reason: k.reason, Rate: k.rate,
			BasisPoints: k.basisPoints, Currency: k.currency, HalfDay: k.half, Unit: k.unit}
		b.lines[k] = l
		b.order = append(b.order, k)
	}
	l.Quantity += qty
	b.direct[k] += amount
}

func (b *earningsBuilder) workLine(r PayRule) lineKey {
	return lineKey{rule: r.ID, project: r.ProjectID, currency: r.Currency, basis: r.Basis, rate: r.Amount, reason: ReasonWork}
}

// paid is worked minutes of one entry, after breaks.
type paid struct {
	e       Entry
	minutes int
}

// paidWork returns the worked minutes of a day by entry: breaks are taken off the work entries in proportion, and a
// plan day without booked work gets the plan's minutes (net of deviations, which are entries) as one entry.
func (b *earningsBuilder) paidWork(day Day) []paid {
	var work []paid
	breaks, total := 0, 0
	for _, e := range b.byDate[day.Date] {
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
		planned := day.Worked - day.Standby // standby is paid by its own rules
		for _, w := range work {
			planned -= w.minutes
		}
		work = append(work, paid{Entry{EmployerID: b.v.emp.ID, Date: day.Date, Kind: EntryWork}, planned})
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
	return work
}

// rateOf returns the hourly rate of an entry: its snapshot, else the hourly rule of its day.
func (b *earningsBuilder) rateOf(e Entry, day Date) *RateSnapshot {
	if e.Rate != nil {
		return e.Rate
	}
	if r, ok := RuleAt(b.hourly, e.ProjectID, day); ok {
		return &RateSnapshot{RuleID: r.ID, Amount: r.Amount, Currency: r.Currency}
	}
	return nil
}

// work adds the pay for time worked: hourly, per assignment, per project and per period.
func (b *earningsBuilder) work() {
	assignment := rulesFor(b.d.Catalog, b.v.emp.ID, PayPerAssignment)
	seenAssignments := map[string]bool{}
	for _, day := range b.days {
		for _, w := range b.paidWork(day) {
			if w.minutes == 0 {
				continue
			}
			if rate := b.rateOf(w.e, day.Date); rate != nil {
				b.add(lineKey{rule: rate.RuleID, currency: rate.Currency, basis: PayHourly, rate: rate.Amount,
					reason: ReasonWork}, w.minutes, 0)
			}
		}
		for _, r := range assignment {
			if !day.Date.Within(r.ValidFrom, r.ValidTo) {
				continue
			}
			if r.Unit == UnitAssignment {
				for _, e := range b.byDate[day.Date] {
					if e.Kind == EntryWork && e.Assignment != "" && (r.ProjectID == "" || e.ProjectID == r.ProjectID) &&
						!seenAssignments[r.ID+"\x00"+e.Assignment] {
						seenAssignments[r.ID+"\x00"+e.Assignment] = true
						b.add(b.workLine(r), 1, 0)
					}
				}
				continue
			}
			worked := day.Worked-day.Standby > 0
			if r.ProjectID != "" {
				worked = day.Projects[r.ProjectID] > 0
			}
			if !worked {
				continue
			}
			k := b.workLine(r)
			if day.Part != "" {
				k.half, k.rate = true, r.HalfDayAmount()
			}
			b.add(k, 1, 0)
		}
	}

	// Per project, on completion.
	for _, r := range rulesFor(b.d.Catalog, b.v.emp.ID, PayPerProject) {
		for _, p := range b.d.Catalog.Projects {
			if p.ID == r.ProjectID && !p.CompletedOn.IsZero() && p.CompletedOn.Within(b.from, b.to) &&
				p.CompletedOn.Within(r.ValidFrom, r.ValidTo) {
				b.add(b.workLine(r), 1, 0)
			}
		}
	}

	// Flat per period, prorated by days.
	for _, r := range rulesFor(b.d.Catalog, b.v.emp.ID, PayFlatPeriod) {
		unit := r.Unit
		if unit == "" {
			unit = UnitMonth
		}
		start, end := b.from, b.to
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
			k := b.workLine(r)
			k.direct = true
			amount := r.Amount
			if unit == UnitMonth && r.Salaries > 12 {
				amount = roundDiv(amount*int64(r.Salaries), 12)
			}
			b.add(k, covered, roundDiv(int64(covered)*amount, int64(length)))
			p = pe.AddDays(1)
		}
	}
}

// standby adds the pay for standby (DayInfo): passive and active, per hour, per day with standby of the kind, or per
// call-out. Standby that counts as working time (Day.Standby) is paid here only, never at the hourly rule.
func (b *earningsBuilder) standby() {
	for _, basis := range []PayBasis{PayStandbyPassive, PayStandbyActive} {
		rules := rulesFor(b.d.Catalog, b.v.emp.ID, basis)
		if len(rules) == 0 {
			continue
		}
		for _, day := range b.days {
			info := b.v.infos[day.Date]
			minutes := info.StandbyPassive
			if basis == PayStandbyActive {
				minutes = info.StandbyActive
			}
			r, ok := RuleAt(rules, "", day.Date)
			if !ok || (minutes == 0 && (basis != PayStandbyActive || info.Callouts == 0)) {
				continue
			}
			k := b.workLine(r)
			k.reason, k.unit = string(basis), r.Unit
			switch r.Unit {
			case "":
				b.add(k, minutes, 0)
			case UnitDay:
				b.add(k, 1, 0)
			case UnitAssignment:
				b.add(k, max(1, info.Callouts), 0)
			}
		}
	}
}

// averageKey identifies a reference period of PayAverage.
type averageKey struct {
	start Date
	weeks int
}

// average is what the reference period before an absence earned: amounts per currency (hourly and per assignment),
// the worked minutes and the days with work.
type average struct {
	amounts map[string]int64
	minutes int
	days    int
}

func (b *earningsBuilder) averageBefore(start Date, weeks int) average {
	k := averageKey{start, weeks}
	if a, ok := b.averages[k]; ok {
		return a
	}
	a := average{amounts: map[string]int64{}}
	ref, err := newEarningsBuilder(b.d, b.v.emp.ID, start.AddDays(-7*weeks), start.AddDays(-1))
	if err == nil {
		ref.work()
		for _, l := range ref.finish().Lines {
			if l.Basis == PayHourly || l.Basis == PayPerAssignment {
				a.amounts[l.Currency] += l.Amount
			}
		}
		for _, day := range ref.days {
			worked := day.Worked - day.Standby // the average pays work; standby has its own rules
			a.minutes += worked
			a.days += boolInt(worked > 0)
		}
	}
	b.averages[k] = a
	return a
}

// absencePayRule returns the rule that pays the absence type on the day: the one with the latest valid-from date.
func absencePayRule(rules []AbsencePay, typ string, d Date) (AbsencePay, bool) {
	var best AbsencePay
	found := false
	for _, r := range rules {
		if r.Type == typ && d.Within(r.ValidFrom, r.ValidTo) && (!found || !r.ValidFrom.Before(best.ValidFrom)) {
			best, found = r, true
		}
	}
	return best, found
}

// absences adds the pay for absent time: the target lost to a public holiday and to paid absence types.
func (b *earningsBuilder) absences() {
	var rules []AbsencePay
	for _, r := range b.d.Catalog.AbsencePays {
		if r.EmployerID == b.v.emp.ID {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return
	}
	for _, day := range b.days {
		_, holiday := b.v.holidays.on(day.Date)
		available := day.Target - part(day.Target, holiday)
		if holiday > 0 && day.HasTarget {
			if r, ok := absencePayRule(rules, PublicHolidayPay, day.Date); ok {
				b.payAbsent(r, day, day.Date, part(day.Target, holiday), holiday)
			}
		}
		for _, p := range b.v.absentParts(day.Date) {
			r, ok := absencePayRule(rules, p.absence.Type, day.Date)
			if !ok {
				continue
			}
			minutes := -1 // unknown without a target: the average day decides
			if day.HasTarget && day.Target > 0 {
				minutes = part(available, p.fraction)
			}
			b.payAbsent(r, day, p.absence.From, minutes, p.fraction)
		}
	}
}

// payAbsent pays absent minutes of a day by the rule; start is the first day of the absence (the end of an average's
// reference). Minutes of -1 mean: no target, use the average worked day of the reference times the fraction.
func (b *earningsBuilder) payAbsent(r AbsencePay, day Day, start Date, minutes, fraction int) {
	if r.Method == PayTarget {
		if minutes <= 0 {
			return
		}
		if h, ok := RuleAt(b.hourly, "", day.Date); ok {
			b.add(lineKey{rule: r.ID, currency: h.Currency, basis: PayAbsence, rate: h.Amount, reason: r.Type},
				minutes, 0)
		}
		return
	}
	a := b.averageBefore(start, r.referenceWeeks())
	if a.minutes <= 0 {
		return
	}
	if minutes < 0 {
		minutes = part(int(roundDiv(int64(a.minutes), int64(a.days))), fraction)
	}
	if minutes == 0 {
		return
	}
	for currency, amount := range a.amounts {
		k := lineKey{rule: r.ID, currency: currency, basis: PayAbsence, reason: r.Type, direct: true, ref: start,
			rate: roundDiv(amount*60, int64(a.minutes))}
		b.add(k, minutes, roundDiv(int64(minutes)*amount, int64(a.minutes)))
	}
}

// surchargesOf returns the surcharges of the employer with the kind valid on the day.
func (b *earningsBuilder) surchargesOf(kind SurchargeKind, d Date) []Surcharge {
	var out []Surcharge
	for _, s := range b.d.Catalog.Surcharges {
		if s.EmployerID == b.v.emp.ID && s.Kind == kind && d.Within(s.ValidFrom, s.ValidTo) {
			out = append(out, s)
		}
	}
	return out
}

// surcharge adds minutes of a surcharge, at its base or at the entry's rate.
func (b *earningsBuilder) surcharge(s Surcharge, rate *RateSnapshot, minutes int) {
	if minutes <= 0 {
		return
	}
	k := lineKey{rule: s.ID, basis: PaySurcharge, reason: string(s.Kind), basisPoints: s.BasisPoints}
	switch {
	case s.BaseAmount > 0:
		k.rate, k.currency = s.BaseAmount, s.Currency
	case rate != nil:
		k.rate, k.currency = rate.Amount, rate.Currency
	default:
		return
	}
	b.add(k, minutes, 0)
}

// surcharges adds the night, Sunday, holiday and overtime surcharges.
func (b *earningsBuilder) surcharges() {
	if len(b.d.Catalog.Surcharges) == 0 {
		return
	}
	loc, err := b.v.emp.Location()
	if err != nil {
		return
	}
	fullHoliday := func(d Date) bool { _, f := b.v.holidays.on(d); return f >= 1000 }
	for _, day := range b.days {
		// Timed breaks are cut out of the spans; untimed ones shrink every count of the day in proportion.
		var cuts []interval
		gross, untimed := 0, 0
		for _, e := range b.byDate[day.Date] {
			switch {
			case e.Kind == EntryBreak && e.Start != nil && e.End != nil:
				cuts = append(cuts, interval{*e.Start, *e.End})
			case e.Kind == EntryBreak:
				untimed += e.Minutes
			}
		}
		work := b.paidWork(day)
		for _, w := range work {
			if w.e.Kind == EntryWork {
				gross += w.e.Minutes
			}
		}
		netOf := func(m time.Duration) int {
			minutes := int(math.Round(m.Minutes()))
			if untimed == 0 || gross == 0 {
				return minutes
			}
			return int(roundDiv(int64(minutes)*int64(max(0, gross-untimed)), int64(gross)))
		}
		for _, w := range work {
			if w.e.Kind != EntryWork || w.minutes <= 0 {
				continue
			}
			rate := b.rateOf(w.e, day.Date)
			var parts []interval
			if w.e.Start != nil && w.e.End != nil {
				parts = subtract(interval{*w.e.Start, *w.e.End}, cuts)
			}
			// The minutes of the entry in a window: by the clock for spans, by the day's date otherwise.
			in := func(clock func(interval) time.Duration, byDate bool) int {
				if parts == nil {
					return boolInt(byDate) * w.minutes
				}
				var total time.Duration
				for _, p := range parts {
					total += clock(p)
				}
				return netOf(total)
			}
			holidayRules := b.surchargesOf(SurchargeHoliday, day.Date)
			for _, s := range holidayRules {
				b.surcharge(s, rate, in(func(p interval) time.Duration { return dayMinutes(p, loc, fullHoliday) },
					fullHoliday(day.Date)))
			}
			sunday := func(d Date) bool {
				return d.Weekday() == time.Sunday && !(len(holidayRules) > 0 && fullHoliday(d))
			}
			for _, s := range b.surchargesOf(SurchargeSunday, day.Date) {
				b.surcharge(s, rate, in(func(p interval) time.Duration { return dayMinutes(p, loc, sunday) },
					sunday(day.Date)))
			}
			for _, s := range b.surchargesOf(SurchargeNight, day.Date) {
				b.surcharge(s, rate, in(func(p interval) time.Duration {
					return windowMinutes(p, loc, s.WindowStart, s.WindowEnd)
				}, false))
			}
		}
	}
	for _, p := range overtimePeriods(b.days, b.d.Catalog, b.v.emp) {
		if p.surplus <= 0 {
			continue
		}
		var rate *RateSnapshot
		if r, ok := RuleAt(b.hourly, "", p.to); ok {
			rate = &RateSnapshot{RuleID: r.ID, Amount: r.Amount, Currency: r.Currency}
		}
		for _, s := range b.surchargesOf(SurchargeOvertime, p.to) {
			b.surcharge(s, rate, p.surplus)
		}
	}
}

// finish computes the amounts, orders the lines by basis and sums the totals.
func (b *earningsBuilder) finish() Earnings {
	out := Earnings{EmployerID: b.v.emp.ID, From: b.from, To: b.to, Total: map[string]int64{}}
	type reasonKey struct {
		basis            PayBasis
		reason, currency string
	}
	reasons := map[reasonKey]int64{}
	var reasonOrder []reasonKey
	for _, k := range b.order {
		l := b.lines[k]
		switch {
		case k.direct:
			l.Amount = b.direct[k]
		case l.Basis == PayHourly || l.Basis == PayAbsence ||
			((l.Basis == PayStandbyPassive || l.Basis == PayStandbyActive) && l.Unit == ""):
			l.Amount = roundDiv(int64(l.Quantity)*l.Rate, 60)
		case l.Basis == PaySurcharge:
			l.Amount = roundDiv(int64(l.Quantity)*l.Rate*int64(l.BasisPoints), 60*10000)
		default:
			l.Amount = int64(l.Quantity) * l.Rate
		}
		out.Lines = append(out.Lines, *l)
		out.Total[l.Currency] += l.Amount
		rk := reasonKey{l.Basis, l.Reason, l.Currency}
		if _, ok := reasons[rk]; !ok {
			reasonOrder = append(reasonOrder, rk)
		}
		reasons[rk] += l.Amount
	}
	byBasis := func(a, b PayBasis) int { return strings.Compare(string(a), string(b)) }
	slices.SortStableFunc(out.Lines, func(a, b EarningLine) int { return byBasis(a.Basis, b.Basis) })
	for _, rk := range reasonOrder {
		out.Reasons = append(out.Reasons, ReasonTotal{Basis: rk.basis, Reason: rk.reason, Currency: rk.currency,
			Amount: reasons[rk]})
	}
	slices.SortStableFunc(out.Reasons, func(a, b ReasonTotal) int {
		if c := byBasis(a.Basis, b.Basis); c != 0 {
			return c
		}
		return strings.Compare(a.Reason, b.Reason)
	})
	return out
}

// ComputeEarnings calculates what the employer pays from from to to, as lines by basis and reason:
//   - hourly: worked minutes times the rate. An entry uses its snapshot, else the rule of its date; days of the mode
//     plan without entries use the employer's rule. Breaks of a day are taken off its work entries in proportion.
//   - per_project: the amount once, when the project is completed in the range.
//   - per_assignment: the amount per worked day (unit day) or per distinct assignment of the entries (unit assignment);
//     a day whose work took only its morning or its afternoon (Day.Part) pays the rule's amount for half a day.
//   - flat_period: the amount per week, month or year, prorated by days for a part of it.
//   - standby_passive, standby_active: the standby of DayInfo per hour, per day with standby or per call-out.
//   - paid_absence: absent time that an AbsencePay rule pays (public holidays, leave, sickness, own types).
//   - surcharge: the Surcharge rules on night, Sunday, holiday and overtime minutes.
//   - income: the Income records of the range (a job with its client, commission, photos, tips), as booked.
//
// Entries count for the day DaySplit gives them. PayAverage needs the entries of its reference weeks in d (see
// EarningsDataFrom); overtime surcharges look at the whole periods of the overtime basis, so ask for whole periods.
func ComputeEarnings(d Data, employerID string, from, to Date) (Earnings, error) {
	b, err := newEarningsBuilder(d, employerID, from, to)
	if err != nil {
		return Earnings{}, err
	}
	b.work()
	b.standby()
	b.absences()
	b.surcharges()
	taken := b.incomes()
	out := b.finish()
	if len(taken) > 0 {
		out.Taken = taken
	}
	return out, nil
}

// EarningsDataFrom returns the first day ComputeEarnings needs entries from, for a range that starts on from: the day
// before (a night shift reaching into from), or, when an absence pay rule averages, a year plus the longest reference
// before (an absence that reaches into the range can have started up to a year earlier).
func EarningsDataFrom(cat Catalog, employerID string, from Date) Date {
	weeks := 0
	for _, r := range cat.AbsencePays {
		if r.EmployerID == employerID && r.Method == PayAverage {
			weeks = max(weeks, r.referenceWeeks())
		}
	}
	if weeks == 0 {
		return from.AddDays(-1)
	}
	return from.AddDays(-367 - 7*weeks)
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
