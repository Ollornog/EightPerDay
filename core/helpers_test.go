// SPDX-License-Identifier: MIT

package core

import (
	"testing"
	"time"
)

const owner = "alice"

func d(s string) Date { return MustDate(s) }

func employerOf(mode Mode, region string) Employer {
	return Employer{ID: "e1", Owner: owner, Name: "Example GmbH", Kind: KindEmployer, Mode: mode, Region: region,
		WeekStart: time.Monday}
}

// week builds a schedule from minutes for Monday to Sunday.
func week(id, from string, minutes ...int) Schedule {
	s := Schedule{ID: id, Owner: owner, EmployerID: "e1", ValidFrom: d(from), LeaveDays: 30}
	for i, m := range minutes {
		s.Days[(i+1)%7].Minutes = m
	}
	return s
}

func work(date string, minutes int) Entry {
	return Entry{ID: NewID(), Owner: owner, EmployerID: "e1", Kind: EntryWork, Date: d(date), Minutes: minutes,
		Precision: PrecisionDay, Source: SourceManual}
}

func withKind(e Entry, k EntryKind) Entry {
	e.Kind = k
	return e
}

func absence(typ, from, to string) Absence {
	return Absence{ID: NewID(), Owner: owner, EmployerID: "e1", Type: typ, From: d(from), To: d(to),
		Status: StatusApproved}
}

func mustDays(t *testing.T, data Data, from, to string) []Day {
	t.Helper()
	days, err := Days(data, "e1", d(from), d(to))
	if err != nil {
		t.Fatal(err)
	}
	return days
}

func dayOf(t *testing.T, days []Day, date string) Day {
	t.Helper()
	for _, x := range days {
		if x.Date == d(date) {
			return x
		}
	}
	t.Fatalf("no day %s", date)
	return Day{}
}
