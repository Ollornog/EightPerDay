// SPDX-License-Identifier: MIT

package core

import "strings"

// Income and the details of a day: what people in seasonal and freelance work keep next to their hours. A pilot of
// tandem flights is paid per flight with a customer and an amount, adds the photos and a tip, and takes some in cash
// and some by card; a ski instructor notes where the day was and how far the drive.

// IncomeKind says what an income is for.
type IncomeKind string

const (
	// IncomeJob is the amount of one job, with its client (a flight, a lesson, a ride).
	IncomeJob        IncomeKind = "job"
	IncomeCommission IncomeKind = "commission"
	IncomePhotos     IncomeKind = "photos"
	IncomeTip        IncomeKind = "tip"
	IncomeOther      IncomeKind = "other"
)

// IncomeKinds lists the kinds of income.
var IncomeKinds = []IncomeKind{IncomeJob, IncomeCommission, IncomePhotos, IncomeTip, IncomeOther}

// PaidBy says how an income was taken.
type PaidBy string

const (
	PaidCash     PaidBy = "cash"
	PaidCard     PaidBy = "card"
	PaidTransfer PaidBy = "transfer"
)

// PayIncome: an income booked as it was earned (EarningLine.Reason names the IncomeKind).
const PayIncome PayBasis = "income"

// Income is money earned on a day of an employer beyond what its pay rules compute: the amount of a job with its
// client, a commission, photos sold, a tip. ComputeEarnings lists it as a line of its own.
type Income struct {
	ID         string     `json:"id"`
	Owner      string     `json:"owner"`
	EmployerID string     `json:"employer_id"`
	Date       Date       `json:"date"`
	Kind       IncomeKind `json:"kind"`
	// Client is the customer of a job, as free text.
	Client   string `json:"client,omitempty"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	PaidBy   PaidBy `json:"paid_by,omitempty"`
	Note     string `json:"note,omitempty"`
}

// Meta files the income.
func (i Income) Meta() Meta { return Meta{RecIncome, i.ID, i.Owner, i.EmployerID} }

// Validate checks the income.
func (i Income) Validate() error {
	if err := checkKeys(i.ID, i.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", i.EmployerID, true); err != nil {
		return err
	}
	if i.Date.IsZero() {
		return invalid("income without a day")
	}
	switch i.Kind {
	case IncomeJob, IncomeCommission, IncomePhotos, IncomeTip, IncomeOther:
	default:
		return invalid("income kind %q", i.Kind)
	}
	switch i.PaidBy {
	case "", PaidCash, PaidCard, PaidTransfer:
	default:
		return invalid("paid by %q", i.PaidBy)
	}
	if i.Amount <= 0 {
		return invalid("an income needs an amount")
	}
	if !currencyPattern.MatchString(i.Currency) {
		return invalid("currency %q", i.Currency)
	}
	if len(i.Client) > 200 || len(i.Note) > 200 {
		return invalid("client or note longer than 200 characters")
	}
	return nil
}

// DayInfo holds the details of one day of an employer: where the work was, how far the way, and standby. One per
// employer and day (DayInfoID).
type DayInfo struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	EmployerID string `json:"employer_id"`
	Date       Date   `json:"date"`
	Place      string `json:"place,omitempty"`
	// KmOneWay and KmTotal are kilometres in tenths: the way there, and all of the day.
	KmOneWay int `json:"km_one_way,omitempty"`
	KmTotal  int `json:"km_total,omitempty"`
	// StandbyPassive and StandbyActive are minutes on standby: reachable only, and called out.
	StandbyPassive int `json:"standby_passive,omitempty"`
	StandbyActive  int `json:"standby_active,omitempty"`
	// Callouts counts the call-outs of active standby, for pay per call-out; zero with active minutes counts as one.
	Callouts int `json:"callouts,omitempty"`
}

// DayInfoID is the ID of the details of an employer's day, so a store replaces them instead of adding.
func DayInfoID(employerID string, d Date) string {
	return employerID + "-" + strings.ReplaceAll(d.String(), "-", "")
}

// Meta files the details of the day.
func (i DayInfo) Meta() Meta { return Meta{RecDayInfo, i.ID, i.Owner, i.EmployerID} }

// Validate checks the details of the day.
func (i DayInfo) Validate() error {
	if err := checkKeys(i.ID, i.Owner); err != nil {
		return err
	}
	if err := checkRef("employer", i.EmployerID, true); err != nil {
		return err
	}
	if i.Date.IsZero() || i.ID != DayInfoID(i.EmployerID, i.Date) {
		return invalid("day details need their day and the ID of it")
	}
	if len(i.Place) > 200 {
		return invalid("place longer than 200 characters")
	}
	if i.KmOneWay < 0 || i.KmTotal < 0 || i.KmOneWay > 100000 || i.KmTotal > 100000 {
		return invalid("kilometres out of range")
	}
	if i.StandbyPassive < 0 || i.StandbyActive < 0 || i.StandbyPassive+i.StandbyActive > 24*60 {
		return invalid("standby out of range")
	}
	if i.Callouts < 0 || i.Callouts > 100 {
		return invalid("call-outs out of range")
	}
	return nil
}

// incomes adds the incomes of the range as lines, and sums what was taken in cash, by card or by transfer.
func (b *earningsBuilder) incomes() map[PaidBy]map[string]int64 {
	taken := map[PaidBy]map[string]int64{}
	for _, i := range b.d.Catalog.Incomes {
		if i.EmployerID != b.v.emp.ID || !i.Date.Within(b.from, b.to) {
			continue
		}
		k := lineKey{currency: i.Currency, basis: PayIncome, reason: string(i.Kind), direct: true}
		b.add(k, 1, i.Amount)
		if i.PaidBy != "" {
			if taken[i.PaidBy] == nil {
				taken[i.PaidBy] = map[string]int64{}
			}
			taken[i.PaidBy][i.Currency] += i.Amount
		}
	}
	return taken
}
