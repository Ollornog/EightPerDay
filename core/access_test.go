// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAccessChain(t *testing.T) {
	var log []Access
	prev := ""
	for _, what := range []string{"entries", "absences", "export"} {
		a, err := PrepareAccess(Access{Actor: "boss", Organization: "acme", Subject: owner, Action: AccessRead,
			What: what, At: time.Date(2026, 3, 9, 8, 0, 0, 123456789, time.UTC)}, prev)
		if err != nil {
			t.Fatal(err)
		}
		if a.At.Nanosecond() != 123456000 {
			t.Fatalf("time not cut to microseconds: %v", a.At)
		}
		log = append(log, a)
		prev = a.Hash
	}
	if err := VerifyAccesses(log); err != nil {
		t.Fatal(err)
	}
	// The first entry may follow a purged one.
	if err := VerifyAccesses(log[1:]); err != nil {
		t.Fatalf("after a purge: %v", err)
	}
	for name, tamper := range map[string]func([]Access) []Access{
		"changed":            func(l []Access) []Access { l[1].What = "nothing"; return l },
		"other actor":        func(l []Access) []Access { l[0].Actor = "nobody"; return l },
		"removed in between": func(l []Access) []Access { return append(l[:1], l[2]) },
		"reordered":          func(l []Access) []Access { l[1], l[2] = l[2], l[1]; return l },
		"another subject":    func(l []Access) []Access { l[2].Subject = "bob"; return l },
	} {
		l := tamper(append([]Access(nil), log...))
		if err := VerifyAccesses(l); !errors.Is(err, ErrTampered) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, a := range map[string]Access{
		"no actor":       {Subject: owner, Action: AccessRead, What: "x"},
		"no subject":     {Actor: "boss", Action: AccessRead, What: "x"},
		"unknown action": {Actor: "boss", Subject: owner, Action: "peek", What: "x"},
		"nothing named":  {Actor: "boss", Subject: owner, Action: AccessRead, What: " "},
		"backwards":      {Actor: "boss", Subject: owner, Action: AccessRead, What: "x", From: d("2026-03-02"), To: d("2026-03-01")},
	} {
		if _, err := PrepareAccess(a, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted", name)
		}
	}
	if err := (Retention{Class: ClassAccessLog, YearsAfterYearEnd: 3}).Validate(); err != nil {
		t.Fatal(err)
	}
}

// sliceLog is an access log in a slice, for the tests of the package.
type sliceLog struct{ entries []Access }

func (l *sliceLog) LogAccess(_ context.Context, a Access) (Access, error) {
	prev := ""
	for _, e := range l.entries {
		if e.Subject == a.Subject {
			prev = e.Hash
		}
	}
	a, err := PrepareAccess(a, prev)
	if err == nil {
		l.entries = append(l.entries, a)
	}
	return a, err
}

func (l *sliceLog) Accesses(_ context.Context, subject string) ([]Access, error) {
	var out []Access
	for _, e := range l.entries {
		if e.Subject == subject {
			out = append(out, e)
		}
	}
	return out, nil
}

// LogAccessOnce adds a read only when nothing of the same day stands for it: another actor, organization, employer,
// action, purpose, more days or another day each add an entry, and the chain holds.
func TestLogAccessOnce(t *testing.T) {
	ctx := context.Background()
	log := &sliceLog{}
	at := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	base := Access{At: at, Actor: "boss", Organization: "acme", Subject: owner, Action: AccessRead, What: "approval",
		From: d("2026-11-01"), To: d("2026-11-30")}
	if _, added, err := LogAccessOnce(ctx, log, base, nil); err != nil || !added {
		t.Fatalf("the first read was not logged: %v %v", added, err)
	}
	again := base
	again.At = at.Add(10 * time.Hour)
	if first, added, err := LogAccessOnce(ctx, log, again, nil); err != nil || added || !first.At.Equal(at) {
		t.Fatalf("the same view later that day: added %v, %+v %v", added, first, err)
	}
	for name, change := range map[string]func(*Access){
		"another actor":        func(a *Access) { a.Actor = "hr" },
		"another organization": func(a *Access) { a.Organization = "other" },
		"another employer":     func(a *Access) { a.EmployerID = "m1" },
		"another action":       func(a *Access) { a.Action = AccessExport },
		"another purpose":      func(a *Access) { a.What = "presence view" },
		"a day before":         func(a *Access) { a.From = d("2026-10-31") },
		"a day after":          func(a *Access) { a.To = d("2026-12-01") },
		"no days":              func(a *Access) { a.From, a.To = Date{}, Date{} },
		"the next day":         func(a *Access) { a.At = at.Add(24 * time.Hour) },
	} {
		a := base
		a.At = at.Add(time.Hour)
		change(&a)
		n := len(log.entries)
		if _, added, err := LogAccessOnce(ctx, log, a, nil); err != nil || !added || len(log.entries) != n+1 {
			t.Errorf("%s was not logged: %v %v", name, added, err)
		}
	}
	// An entry of a later day, written out of order, does not stand for a read today.
	later := &sliceLog{}
	tomorrow := base
	tomorrow.At = at.Add(24 * time.Hour)
	if _, _, err := LogAccessOnce(ctx, later, tomorrow, nil); err != nil {
		t.Fatal(err)
	}
	if _, added, err := LogAccessOnce(ctx, later, base, nil); err != nil || !added {
		t.Errorf("a read today was taken for the one of tomorrow: %v %v", added, err)
	}
	all, _ := log.Accesses(ctx, owner)
	if err := VerifyAccesses(all); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LogAccessOnce(ctx, log, Access{Actor: "boss", Subject: owner, Action: "peek", What: "x"}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an invalid entry: %v", err)
	}
}

// The range of a review covers every open submission and today, and nothing that is decided or withdrawn.
func TestReviewRange(t *testing.T) {
	today := d("2026-11-18")
	c := Catalog{Submissions: []Submission{
		{Status: SubmissionSubmitted, From: d("2026-01-05"), To: d("2026-01-11")},
		{Status: SubmissionSubmitted, From: d("2028-07-01"), To: d("2028-07-14")},
		{Status: SubmissionApproved, From: d("2020-01-01"), To: d("2020-01-01")},
		{Status: SubmissionWithdrawn, From: d("2030-01-01"), To: d("2030-01-01")},
	}}
	if from, to := ReviewRange(c, today); from != d("2026-01-05") || to != d("2028-07-14") {
		t.Fatalf("%v – %v", from, to)
	}
	if from, to := ReviewRange(Catalog{}, today); from != today || to != today {
		t.Fatalf("without submissions: %v – %v", from, to)
	}
}
