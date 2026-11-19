// SPDX-License-Identifier: MIT

package core

import (
	"encoding/json"
	"testing"
)

func dayOptions() DayOptions {
	return DayOptions{Default: "p", Keys: map[string]string{"p": "p", "f": "f", "n": "n"}, Projects: []Project{
		{ID: "studio", EmployerID: "f", Name: "Studio"},
		{ID: "haas", EmployerID: "f", Name: "Wedding Haas", Number: "A-2311"},
		{ID: "lenz", EmployerID: "f", Name: "Event Lenz", Number: "A-2318"},
		{ID: "old", EmployerID: "f", Name: "Hall", Archived: true},
		{ID: "maths", EmployerID: "n", Name: "Maths lessons"},
	}}
}

func groupsJSON(t *testing.T, l DayLine) string {
	t.Helper()
	out, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestParseDay(t *testing.T) {
	half := func(a string) *DayCode { return &DayCode{Absence: a, HalfDay: true} }
	whole := func(a string) *DayCode { return &DayCode{Absence: a} }
	cases := []struct {
		in   string
		want DayLine
	}{
		{"5", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 300, Hours: true}}}},
		{"5,5", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 330, Hours: true}}}},
		{"7:30", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 450, Hours: true}}}},
		{"0", DayLine{Groups: []DayGroup{{EmployerID: "p", Hours: true}}}},
		{"8-13:30 30m", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 300, Hours: true, Typed: "8-13:30 30m"}}}},
		{"8-12 Pause 15m", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 225, Hours: true, Typed: "8-12 15m"}}}},
		{"U", DayLine{Groups: []DayGroup{{EmployerID: "p", Code: whole(AbsenceVacation)}}}},
		{"½U 2,5n", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 150, Hours: true, Part: Afternoon, Code: half(AbsenceVacation)}}}},
		{"4 NM ½K", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 240, Hours: true, Part: Afternoon, Code: half(AbsenceSick)}}}},
		{"½ U", DayLine{Groups: []DayGroup{{EmployerID: "p", Code: half(AbsenceVacation)}}}},
		{"4v", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 240, Hours: true, Part: Morning}}}},
		{"5 n1,5", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 300, Hours: true},
			{EmployerID: "n", Key: "n", Minutes: 90, Hours: true}}}},
		{"f3 studio", DayLine{Groups: []DayGroup{{EmployerID: "f", Key: "f", ProjectID: "studio", Minutes: 180, Hours: true}}}},
		{"f9 haas Wedding, 2 cameras", DayLine{Groups: []DayGroup{{EmployerID: "f", Key: "f", ProjectID: "haas", Minutes: 540, Hours: true}},
			Note: "Wedding, 2 cameras"}},
		{"f6 f4 stu edit photos", DayLine{Groups: []DayGroup{{EmployerID: "f", Key: "f", Minutes: 360, Hours: true},
			{EmployerID: "f", Key: "f", ProjectID: "studio", Minutes: 240, Hours: true}}, Note: "edit photos"}},
		{"f3 a-2318", DayLine{Groups: []DayGroup{{EmployerID: "f", Key: "f", ProjectID: "lenz", Minutes: 180, Hours: true}}}},
		{"4 @A-2311 #photo", DayLine{Groups: []DayGroup{{EmployerID: "f", ProjectID: "haas", Minutes: 240, Hours: true, Tags: []string{"photo"}}}}},
		// A word behind hours names a project of that employer first; "lessons" is the employer n's.
		{"n2 lessons", DayLine{Groups: []DayGroup{{EmployerID: "n", Key: "n", ProjectID: "maths", Minutes: 120, Hours: true}}}},
		// Too short a word, an archived project and a word apart from the hours stay in the note.
		{"3 st", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 180, Hours: true}}, Note: "st"}},
		{"3 hall", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 180, Hours: true}}, Note: "hall"}},
		{"U holiday at the lake", DayLine{Groups: []DayGroup{{EmployerID: "p", Code: whole(AbsenceVacation)}}, Note: "holiday at the lake"}},
		{"nU", DayLine{Groups: []DayGroup{{EmployerID: "n", Key: "n", Code: whole(AbsenceVacation)}}}},
		{"just a note", DayLine{Note: "just a note"}},
		// Two values of the same employer in a row are two groups; the caller sums them.
		{"5 3", DayLine{Groups: []DayGroup{{EmployerID: "p", Minutes: 300, Hours: true}, {EmployerID: "p", Minutes: 180, Hours: true}}}},
	}
	for _, c := range cases {
		got, err := ParseDay(c.in, dayOptions())
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if g, w := groupsJSON(t, got), groupsJSON(t, c.want); g != w {
			t.Errorf("%q:\n got %s\nwant %s", c.in, g, w)
		}
	}
}

// A field of one employer reads no keys: f3 there is no value, and the word stays in the note.
func TestParseDayWithoutKeys(t *testing.T) {
	got, err := ParseDay("f3", DayOptions{Default: "p"})
	if err != nil || len(got.Groups) != 0 || got.Note != "f3" {
		t.Errorf("%+v %v", got, err)
	}
}

func TestParseDayRefuses(t *testing.T) {
	for _, in := range []string{"25", "@studio", "U @studio", "8-9 2h", "U 4", "8-8", "#a/", "x\xff"} {
		if got, err := ParseDay(in, dayOptions()); err == nil {
			t.Errorf("%q was taken: %+v", in, got)
		}
	}
}

func TestEmployerKeys(t *testing.T) {
	keys := EmployerKeys([]Employer{{ID: "1", Name: "Praxis Lindner"}, {ID: "2", Name: "Fotostudio Brandt"}, {ID: "3", Name: "Praxis Nord"},
		{ID: "4", Name: "Ärztehaus", Key: "x"}, {ID: "5", Name: "Pr"}, {ID: "6", Name: "Fx", Key: "p"}})
	want := map[string]string{"p": "6", "f": "2", "pr": "1", "x": "4", "a": "3", "b": "5"}
	if len(keys) != len(want) {
		t.Fatalf("%v", keys)
	}
	for k, id := range want {
		if keys[k] != id {
			t.Errorf("key %s is %s, want %s (%v)", k, keys[k], id, keys)
		}
	}
}

func TestMatchProjectAndItsWord(t *testing.T) {
	projects := dayOptions().Projects
	for _, p := range projects[:3] {
		got, ok := MatchProject(projects, ProjectWord(p))
		if !ok || got.ID != p.ID {
			t.Errorf("%s: its word %q finds %v", p.Name, ProjectWord(p), got.ID)
		}
	}
	// Two letters name no project by the beginning of a word; the whole name or the number does.
	if p, ok := MatchProject(projects, "st"); ok {
		t.Errorf("two letters match %s", p.Name)
	}
	if p, ok := MatchProject([]Project{{ID: "x", Name: "Go"}}, "go"); !ok || p.ID != "x" {
		t.Error("a short whole name is not found")
	}
	if ProjectWord(Project{Name: "AB C"}) != "@ABC" {
		t.Error(ProjectWord(Project{Name: "AB C"}))
	}
}

func TestEmployerKeyIsChecked(t *testing.T) {
	e := employerOf(ModeExact, "")
	for key, ok := range map[string]bool{"": true, "f": true, "abc": true, "abcd": false, "F": false, "ä": false, "1": false} {
		e.Key = key
		if err := e.Validate(); (err == nil) != ok {
			t.Errorf("key %q: %v", key, err)
		}
	}
}
