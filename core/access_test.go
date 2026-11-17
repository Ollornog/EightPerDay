// SPDX-License-Identifier: MIT

package core

import (
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
