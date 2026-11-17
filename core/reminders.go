// SPDX-License-Identifier: MIT

package core

import "time"

// LeaveReminder points out leave that is still open towards the end of the year.
//
// In Germany leave lapses at the end of the year (§ 7 Abs. 3 BUrlG) only if the employer asked the person in time,
// concretely and individually, to take it and pointed out that it lapses otherwise (BAG 20.12.2022, 9 AZR 266/20,
// after CJEU C-120/21). The employer keeps that notice as a LeaveNotice with its time; NoticeGiven says whether there
// is one. This is information, not legal advice.
type LeaveReminder struct {
	EmployerID string  `json:"employer_id"`
	Year       int     `json:"year"`
	Remaining  float64 `json:"remaining"`
	// Expires is the last day of the year.
	Expires Date `json:"expires"`
	// DaysLeft counts the days from today to Expires.
	DaysLeft int `json:"days_left"`
	// NoticeGiven: the employer recorded the notice for the year; NotifiedAt is when.
	NoticeGiven bool       `json:"notice_given"`
	NotifiedAt  *time.Time `json:"notified_at,omitempty"`
	// Lapses: there is a notice, so the leave can lapse at Expires; without one it stays. Whether the notice came in
	// time for the person to still take the leave is not judged here.
	Lapses bool `json:"lapses"`
}

// DefaultReminderDays is how many days before the end of the year LeaveReminders starts to remind.
const DefaultReminderDays = 92

// LeaveReminders returns a reminder for the employer if leave of the current year is still open within warnDays
// (DefaultReminderDays if zero) before the end of the year. The person sees it as a reminder to take the leave; the
// organization, on its managed data, as the list of notices it still owes. The data must hold the year's absences.
// Leave carried over into the next year is an Adjustment there and is not reminded of here.
func LeaveReminders(d Data, employerID string, today Date, warnDays int) ([]LeaveReminder, error) {
	if warnDays <= 0 {
		warnDays = DefaultReminderDays
	}
	acc, err := Leave(d, employerID, today.Year)
	if err != nil {
		return nil, err
	}
	expires := NewDate(today.Year, time.December, 31)
	left := today.DaysUntil(expires)
	if acc.Remaining <= 0 || left > warnDays {
		return nil, nil
	}
	r := LeaveReminder{EmployerID: employerID, Year: today.Year, Remaining: acc.Remaining, Expires: expires,
		DaysLeft: left}
	for _, n := range d.Catalog.LeaveNotices {
		if n.EmployerID == employerID && n.Year == today.Year && (r.NotifiedAt == nil || n.NotifiedAt.After(*r.NotifiedAt)) {
			at := n.NotifiedAt
			r.NoticeGiven, r.NotifiedAt = true, &at
		}
	}
	r.Lapses = r.NoticeGiven
	return []LeaveReminder{r}, nil
}
