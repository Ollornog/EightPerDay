// SPDX-License-Identifier: MIT

package core

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The line of a day: everything a person did on one day typed into one field, the way the start page's journal reads
// it and every field of a day reads its part of it. It is Parse's language for one day (durations, spans with a break,
// #tags, @project, the rest a note) with the short codes of a day (U, K, F, ½U, ParseDayCode), the half of a day behind
// the hours (4v, 4 NM), plain numbers as hours (8, 8,5), the key of an employer in front of the hours (f3) and a word
// that names a project behind them (f3 studio).
//
//	5                  five hours for the day's employer
//	8-13:30 30m        a span less its break: five hours, given back as typed
//	½U 2,5n            half a day of leave and two and a half hours in the afternoon
//	5 n1,5             five hours, and one and a half for the employer with the key n
//	f3 studio f6       three hours on the project "Studio" of the employer f, and six hours for f that day
//	4 @A-2311 #photo   four hours on the project with the number A-2311, tagged
//
// Every other word is the note of the day.

// DayGroup is what a line books on one employer: the day's hours of the employer, or the hours on one of its
// projects; a short code; the half of the day.
type DayGroup struct {
	// EmployerID is the employer: the one of the key typed in front, or DayOptions.Default.
	EmployerID string `json:"employer_id"`
	// Key is the key that was typed, empty for the default employer.
	Key string `json:"key,omitempty"`
	// ProjectID is the project the hours are on; empty for the employer's hours of the day.
	ProjectID string `json:"project_id,omitempty"`
	Minutes   int    `json:"minutes"`
	// Hours: the group holds hours, even zero ones (0 clears a day).
	Hours bool     `json:"hours,omitempty"`
	Part  DayPart  `json:"part,omitempty"`
	Code  *DayCode `json:"code,omitempty"`
	// Typed is the span with its break as it was typed (Entry.Typed); empty when the hours were typed as hours.
	Typed string   `json:"typed,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

// DayLine is a line of a day read by ParseDay.
type DayLine struct {
	Groups []DayGroup `json:"groups"`
	Note   string     `json:"note,omitempty"`
}

// DayOptions say what the keys and words of a line of a day stand for.
type DayOptions struct {
	// Default is the employer of hours typed without a key.
	Default string
	// Keys maps the keys of employers (Employer.Key, EmployerKeys) to their IDs. Empty: no key is read, as in the field
	// of one employer.
	Keys map[string]string
	// Projects are the projects a word behind hours may name (MatchProject); those of the employer of the hours are
	// tried first.
	Projects []Project
}

// dayPartWords are the ways the half of a day is written behind the hours: v/vm (Vormittag), n/nm (Nachmittag), a/am
// and p/pm.
var dayPartWords = map[string]DayPart{"v": Morning, "vm": Morning, "a": Morning, "am": Morning,
	"n": Afternoon, "nm": Afternoon, "p": Afternoon, "pm": Afternoon}

// ParseDayPart reads the half of a day as it is written behind hours: v, vm, a or am for the morning, n, nm, p or pm
// for the afternoon, in any case.
func ParseDayPart(word string) (DayPart, bool) {
	part, ok := dayPartWords[strings.ToLower(word)]
	return part, ok
}

// dayValue is one value of a line: hours (with their half of the day or their span) or a short code.
type dayValue struct {
	minutes int
	hours   bool
	part    DayPart
	code    *DayCode
	span    bool
}

// readDayValue reads one word as a value of a day: a short code, a span, or hours without a sign, optionally with the
// half of the day stuck to them (4v, 2,5n, 4nm).
func readDayValue(word string) (dayValue, bool) {
	if code, ok := ParseDayCode(word); ok {
		return dayValue{code: &code}, true
	}
	lower := strings.ToLower(word)
	if m := reSpan.FindStringSubmatch(lower); m != nil {
		start, err1 := clock(m[1], m[2])
		end, err2 := clock(m[3], m[4])
		if err1 != nil || err2 != nil || start == end {
			return dayValue{}, false
		}
		if end < start {
			end += 24 * 60
		}
		return dayValue{minutes: end - start, hours: true, span: true}, true
	}
	body, part := lower, DayPart("")
	for _, suffix := range []string{"vm", "nm", "am", "pm", "v", "n", "a", "p"} {
		if rest, found := strings.CutSuffix(lower, suffix); found && rest != "" && unicode.IsDigit(rune(rest[len(rest)-1])) {
			body, part = rest, dayPartWords[suffix]
			break
		}
	}
	minutes, ok := plainHours(body)
	if !ok {
		return dayValue{}, false
	}
	if minutes == 0 {
		part = ""
	}
	return dayValue{minutes: minutes, hours: true, part: part}, true
}

// plainHours reads hours without a sign: 8, 8,5, 8.5, 7:30, 7h30, 2h, 90m.
func plainHours(word string) (int, bool) {
	if word == "" || word[0] == '+' || word[0] == '-' {
		return 0, false
	}
	if allDecimal(word) {
		word += "h"
	}
	d, ok := parseDuration(word)
	if !ok || d.signed || d.minutes < 0 {
		return 0, false
	}
	return d.minutes, true
}

// allDecimal reports whether a word is a number with an optional decimal part: 8, 8,5, 7.25.
func allDecimal(word string) bool {
	seen := false
	for i, c := range word {
		switch {
		case c >= '0' && c <= '9':
		case (c == ',' || c == '.') && i > 0 && !seen && i < len(word)-1:
			seen = true
		default:
			return false
		}
	}
	return true
}

// withKey splits a word into the key of an employer and a value behind it (f3, nU), when both are what they seem.
func withKey(word string, keys map[string]string) (string, dayValue, bool) {
	lower := strings.ToLower(word)
	for n := 3; n >= 1; n-- {
		if len(lower) <= n {
			continue
		}
		key := lower[:n]
		if _, ok := keys[key]; !ok {
			continue
		}
		if v, ok := readDayValue(word[n:]); ok {
			return key, v, true
		}
	}
	return "", dayValue{}, false
}

// ParseDay reads the line of a day (see DayLine). It refuses what it cannot place: a project behind no hours, a code
// on a project, hours of more than a day.
func ParseDay(input string, o DayOptions) (DayLine, error) {
	var line DayLine
	if !utf8.ValidString(input) {
		return line, &ParseError{Msg: "not valid UTF-8"}
	}
	if len(input) > MaxInput {
		return line, &ParseError{Msg: "too long"}
	}
	tokens := joinHalfMarks(strings.Fields(input))
	var note, pendingTags []string
	var typed [][]string // the words of each group's span and break, as typed
	var cur *DayGroup
	last := -1 // the index of the group the previous word gave a value to, or -1
	breakNext := false
	newGroup := func(key string) {
		emp := o.Default
		if key != "" {
			emp = o.Keys[key]
		}
		line.Groups = append(line.Groups, DayGroup{EmployerID: emp, Key: key, Tags: pendingTags})
		typed = append(typed, nil)
		pendingTags = nil
		cur = &line.Groups[len(line.Groups)-1]
	}
	for _, tok := range tokens {
		word := strings.ToLower(strings.TrimRight(tok, ",;"))
		previous := last
		last = -1
		switch {
		case strings.HasPrefix(tok, "#") && len(tok) > 1:
			tag := strings.TrimRight(tok[1:], ",;")
			if !tagPattern.MatchString(tag) || len(tag) > 200 {
				return line, &ParseError{tok, "not a valid tag"}
			}
			if cur == nil {
				pendingTags = append(pendingTags, tag)
			} else if !containsString(cur.Tags, tag) {
				cur.Tags = append(cur.Tags, tag)
			}
			last = previous
			continue
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			if previous < 0 || cur.ProjectID != "" {
				return line, &ParseError{tok, "a project needs hours in front of it"}
			}
			p, ok := MatchProject(projectsFor(o.Projects, cur.EmployerID), strings.TrimRight(tok[1:], ",;"))
			if !ok {
				return line, &ParseError{tok, "no such project"}
			}
			cur.ProjectID, cur.EmployerID = p.ID, p.EmployerID
			continue
		case breakWords[word]:
			breakNext = true
			last = previous
			continue
		}
		if part, ok := dayPartWords[word]; ok && previous >= 0 && cur.Hours && cur.Part == "" && cur.Minutes > 0 {
			cur.Part = part
			last = previous
			continue
		}
		// A break: a duration behind a span or behind the word Pause.
		if cur != nil && previous >= 0 && (breakNext || typed[len(typed)-1] != nil) {
			if d, ok := parseDuration(word); ok && !d.signed && d.minutes > 0 {
				if d.minutes >= cur.Minutes {
					return line, &ParseError{tok, "the break is as long as the work"}
				}
				cur.Minutes -= d.minutes
				typed[len(typed)-1] = append(typed[len(typed)-1], tok)
				breakNext = false
				last = previous
				continue
			}
		}
		breakNext = false
		if m := reSpan.FindStringSubmatch(word); m != nil && m[1] == m[3] && m[2] == m[4] {
			return line, &ParseError{tok, "start and end are the same"}
		}
		// Values come first, the note last: behind a word of the note only the hours of a key count, so a number in
		// the note stays there.
		key, v, keyed := "", dayValue{}, false
		if len(o.Keys) > 0 {
			key, v, keyed = withKey(tok, o.Keys)
		}
		if !keyed && len(note) == 0 {
			if code, ok := ParseDayCode(tok); ok {
				v, keyed = dayValue{code: &code}, true
			} else {
				v, keyed = readDayValue(tok)
			}
		}
		if keyed {
			switch {
			case key != "":
				newGroup(key)
			case cur != nil && previous >= 0 && cur.ProjectID == "" && combines(*cur, v):
			case cur != nil:
				newGroup(cur.Key)
			default:
				newGroup("")
			}
			if v.code != nil {
				cur.Code = v.code
			}
			if v.hours {
				cur.Minutes += v.minutes
				cur.Hours = true
				if v.part != "" {
					cur.Part = v.part
				}
				if v.span {
					typed[len(typed)-1] = append(typed[len(typed)-1], tok)
				}
			}
			last = len(line.Groups) - 1
			continue
		}
		// A word right behind hours may name a project.
		if previous >= 0 && cur.ProjectID == "" && cur.Hours && cur.Code == nil && utf8.RuneCountInString(word) >= 3 {
			if p, ok := MatchProject(projectsFor(o.Projects, cur.EmployerID), word); ok {
				cur.ProjectID, cur.EmployerID = p.ID, p.EmployerID
				continue
			}
		}
		note = append(note, tok)
	}
	note = append(note, tagWords(pendingTags)...)
	line.Note = strings.Join(note, " ")
	// The day of an employer: one code at most, and hours next to it only when it takes half the day.
	codes, hours := map[string]*DayCode{}, map[string]int{}
	for i := range line.Groups {
		g := &line.Groups[i]
		if g.Minutes > 24*60 {
			return line, &ParseError{Msg: "more than 24 hours"}
		}
		if g.ProjectID != "" && g.Code != nil {
			return line, &ParseError{Msg: "a project takes hours, not a code"}
		}
		if len(typed[i]) > 0 && len(typed[i]) <= 3 {
			g.Typed = strings.Join(typed[i], " ")
		}
		if g.ProjectID != "" {
			continue
		}
		hours[g.EmployerID] += g.Minutes
		if g.Code != nil {
			if codes[g.EmployerID] != nil {
				return line, &ParseError{Msg: "two codes for one day"}
			}
			codes[g.EmployerID] = g.Code
		}
	}
	for emp, code := range codes {
		if !code.HalfDay && hours[emp] > 0 {
			return line, &ParseError{Msg: "hours next to a whole day of absence"}
		}
	}
	return line, nil
}

// combines reports whether a value joins the group before it rather than starting one of its own: half a day of a
// code next to hours (½U 2,5n, 4n ½U).
func combines(g DayGroup, v dayValue) bool {
	switch {
	case v.code != nil:
		return g.Code == nil && v.code.HalfDay && g.Hours
	case v.hours:
		return g.Code != nil && g.Code.HalfDay && !g.Hours
	}
	return false
}

// joinHalfMarks joins a half mark written apart from its code: ½ U.
func joinHalfMarks(tokens []string) []string {
	var out []string
	for i := 0; i < len(tokens); i++ {
		if (tokens[i] == "½" || tokens[i] == "/2") && i+1 < len(tokens) {
			if _, ok := ParseDayCode(tokens[i] + tokens[i+1]); ok {
				out = append(out, tokens[i]+tokens[i+1])
				i++
				continue
			}
		}
		out = append(out, tokens[i])
	}
	return out
}

func tagWords(tags []string) []string {
	var out []string
	for _, t := range tags {
		out = append(out, "#"+t)
	}
	return out
}

// projectsFor puts the projects of the employer first.
func projectsFor(projects []Project, employerID string) []Project {
	var own, other []Project
	for _, p := range projects {
		if p.EmployerID == employerID {
			own = append(own, p)
		} else {
			other = append(other, p)
		}
	}
	return append(own, other...)
}

// MatchProject finds the project a word names: its order number, its whole name, or the beginning (three letters at
// least) of a word of its name, in any case. Archived projects are left out; the first match in the order given wins.
func MatchProject(projects []Project, word string) (Project, bool) {
	w := strings.ToLower(strings.TrimSpace(word))
	if w == "" {
		return Project{}, false
	}
	for _, p := range projects {
		if !p.Archived && (strings.EqualFold(p.Number, w) || strings.EqualFold(p.Name, w)) {
			return p, true
		}
	}
	if utf8.RuneCountInString(w) < 3 {
		return Project{}, false
	}
	for _, p := range projects {
		if p.Archived {
			continue
		}
		for _, part := range strings.FieldsFunc(strings.ToLower(p.Name), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if strings.HasPrefix(part, w) {
				return p, true
			}
		}
	}
	return Project{}, false
}

// ProjectWord is the word a line of a day writes behind hours to name the project: its order number, or else the
// first word of its name with three letters at least, in lower case (MatchProject finds the project by it).
func ProjectWord(p Project) string {
	if p.Number != "" && !strings.ContainsFunc(p.Number, unicode.IsSpace) {
		return p.Number
	}
	for _, part := range strings.FieldsFunc(strings.ToLower(p.Name), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if utf8.RuneCountInString(part) >= 3 {
			return part
		}
	}
	return "@" + strings.ReplaceAll(p.Name, " ", "")
}

// EmployerKeys gives every employer its key: the one it has (Employer.Key), or the first letter of its name not yet
// taken, then its first two letters, then the first free letter a to z. Keys of employers listed earlier win. A key
// may be the letter of a short code (f for a photo studio): f3 is three hours for it, F alone a day off.
func EmployerKeys(employers []Employer) map[string]string {
	keys := map[string]string{} // key → employer ID
	taken := func(k string) bool { _, ok := keys[k]; return ok }
	for _, e := range employers {
		if e.Key != "" && !taken(e.Key) {
			keys[e.Key] = e.ID
		}
	}
	for _, e := range employers {
		if e.Key != "" && keys[e.Key] == e.ID {
			continue
		}
		letters := strings.Map(func(r rune) rune {
			r = unicode.ToLower(r)
			if r >= 'a' && r <= 'z' {
				return r
			}
			return -1
		}, foldUmlauts(e.Name))
		var candidates []string
		if letters != "" {
			candidates = append(candidates, letters[:1])
			if len(letters) > 1 {
				candidates = append(candidates, letters[:2])
			}
		}
		for c := 'a'; c <= 'z'; c++ {
			candidates = append(candidates, string(c))
		}
		for _, c := range candidates {
			if !taken(c) {
				keys[c] = e.ID
				break
			}
		}
	}
	return keys
}

// foldUmlauts writes ä, ö, ü and ß as a, o, u and s, so a name such as Ärztehaus has a key.
func foldUmlauts(s string) string {
	return strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "Ä", "A", "Ö", "O", "Ü", "U", "ß", "s").Replace(s)
}
