// SPDX-License-Identifier: MIT

package core

// Pay beyond the time worked: paid absences and surcharges. Both are rules of the employer with a valid-from date, like
// PayRule; ComputeEarnings lists what they pay as lines of their own, separated by reason.

// Line bases of ComputeEarnings beyond the bases of PayRule. A PayRule never has them.
const (
	// PayAbsence: pay for absent time (EarningLine.Reason names the absence type, or PublicHolidayPay).
	PayAbsence PayBasis = "paid_absence"
	// PaySurcharge: a surcharge on worked time (EarningLine.Reason names the SurchargeKind).
	PaySurcharge PayBasis = "surcharge"
)

// ReasonWork is the EarningLine.Reason of the lines for time worked, assignments, projects and periods.
const ReasonWork = "work"

// PublicHolidayPay is the AbsencePay.Type that pays the target lost to a public holiday (or an own holiday).
const PublicHolidayPay = "public_holiday"

// AbsencePayMethod is how an absent day is paid.
type AbsencePayMethod string

const (
	// PayTarget pays the minutes the schedule would have had on the day at the hourly rule valid on it (the minutes
	// lost, as for public holidays and sickness in Germany: § 2 and § 4 EFZG).
	PayTarget AbsencePayMethod = "target"
	// PayAverage pays the same minutes at the average earnings per worked minute of the reference weeks before the
	// absence starts (as for leave in Germany: § 11 BUrlG, 13 weeks). Without a target on the day, the minutes are the
	// average worked minutes per worked day of the reference. The average counts hourly and per-assignment pay.
	PayAverage AbsencePayMethod = "average"
)

// AbsencePay says that an employer pays an absence type, how, from ValidFrom to ValidTo (open if zero). An absence
// type without a rule is unpaid. Monthly salaries (flat_period) already pay absent days; they need no rule.
type AbsencePay struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	EmployerID string `json:"employer_id"`
	// Type is the key of the absence type (such as "vacation" or "sick"), or PublicHolidayPay.
	Type      string           `json:"type"`
	Method    AbsencePayMethod `json:"method"`
	ValidFrom Date             `json:"valid_from"`
	ValidTo   Date             `json:"valid_to,omitempty"`
	// Weeks is the reference period of PayAverage; zero means 13.
	Weeks int `json:"weeks,omitempty"`
}

// DefaultReferenceWeeks is the reference period of PayAverage when AbsencePay.Weeks is zero (§ 11 BUrlG).
const DefaultReferenceWeeks = 13

func (p AbsencePay) referenceWeeks() int {
	if p.Weeks == 0 {
		return DefaultReferenceWeeks
	}
	return p.Weeks
}

// SurchargeKind is what a surcharge pays for.
type SurchargeKind string

const (
	// SurchargeOvertime: the overtime of each period of the schedule's overtime basis (day, week or month).
	SurchargeOvertime SurchargeKind = "overtime"
	// SurchargeNight: worked minutes in a daily clock window (Surcharge.WindowStart to WindowEnd).
	SurchargeNight SurchargeKind = "night"
	// SurchargeSunday: worked minutes on a Sunday. On a full holiday with a holiday surcharge, that one applies alone.
	SurchargeSunday SurchargeKind = "sunday"
	// SurchargeHoliday: worked minutes on a full public holiday of the region or an own full holiday.
	SurchargeHoliday SurchargeKind = "holiday"
)

// Surcharge is a percentage on top of the pay for some minutes, from ValidFrom to ValidTo (open if zero). Night
// surcharges add up with Sunday and holiday surcharges; a holiday surcharge replaces the Sunday surcharge on the same
// minutes (as § 3b EStG counts them). A minute counts for the surcharge by its own clock time and date, whatever day
// its entry counts for (DaySplit). Spans give the clock time; entries without one only count for Sunday and holiday
// surcharges by their date, and plan days by theirs.
type Surcharge struct {
	ID         string        `json:"id"`
	Owner      string        `json:"owner"`
	EmployerID string        `json:"employer_id"`
	Kind       SurchargeKind `json:"kind"`
	ValidFrom  Date          `json:"valid_from"`
	ValidTo    Date          `json:"valid_to,omitempty"`
	// BasisPoints is the percentage in hundredths of a percent: 2500 is 25 %.
	BasisPoints int `json:"basis_points"`
	// BaseAmount is the hourly base in minor units (with Currency), for employers without an hourly rule; zero means
	// the hourly rate of each minute (the entry's snapshot or the hourly rule).
	BaseAmount int64  `json:"base_amount,omitempty"`
	Currency   string `json:"currency,omitempty"`
	// WindowStart and WindowEnd are minutes after local midnight, for night surcharges; a start after the end wraps
	// over midnight (20:00 to 06:00 is 1200 to 360).
	WindowStart int `json:"window_start,omitempty"`
	WindowEnd   int `json:"window_end,omitempty"`
}

// Meta files the absence pay rule.
func (p AbsencePay) Meta() Meta { return Meta{RecAbsencePay, p.ID, p.Owner, p.EmployerID} }

// Meta files the surcharge.
func (s Surcharge) Meta() Meta { return Meta{RecSurcharge, s.ID, s.Owner, s.EmployerID} }

// Validate checks the absence pay rule. The absence type is not checked against the catalog: a rule may come first.
func (p AbsencePay) Validate() error {
	if err := checkKeys(p.ID, p.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", p.EmployerID, true); err != nil {
		return err
	}
	if !idPattern.MatchString(p.Type) {
		return invalid("absence type key %q", p.Type)
	}
	switch p.Method {
	case PayTarget, PayAverage:
	default:
		return invalid("absence pay method %q", p.Method)
	}
	if p.ValidFrom.IsZero() || (!p.ValidTo.IsZero() && p.ValidTo.Before(p.ValidFrom)) {
		return invalid("absence pay needs a valid-from date before its end")
	}
	if p.Weeks < 0 || p.Weeks > 104 {
		return invalid("reference of %d weeks", p.Weeks)
	}
	return nil
}

// Validate checks the surcharge.
func (s Surcharge) Validate() error {
	if err := checkKeys(s.ID, s.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", s.EmployerID, true); err != nil {
		return err
	}
	switch s.Kind {
	case SurchargeOvertime, SurchargeSunday, SurchargeHoliday:
		if s.WindowStart != 0 || s.WindowEnd != 0 {
			return invalid("only a night surcharge has a window")
		}
	case SurchargeNight:
		if s.WindowStart < 0 || s.WindowStart > 24*60 || s.WindowEnd < 0 || s.WindowEnd > 24*60 ||
			s.WindowStart == s.WindowEnd {
			return invalid("night window %d to %d", s.WindowStart, s.WindowEnd)
		}
	default:
		return invalid("surcharge kind %q", s.Kind)
	}
	if s.ValidFrom.IsZero() || (!s.ValidTo.IsZero() && s.ValidTo.Before(s.ValidFrom)) {
		return invalid("surcharge needs a valid-from date before its end")
	}
	if s.BasisPoints <= 0 || s.BasisPoints > 100000 {
		return invalid("surcharge of %d basis points", s.BasisPoints)
	}
	if s.BaseAmount < 0 {
		return invalid("negative base amount")
	}
	if (s.BaseAmount > 0 || s.Currency != "") && !currencyPattern.MatchString(s.Currency) {
		return invalid("currency %q", s.Currency)
	}
	return nil
}
