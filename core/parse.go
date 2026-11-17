// SPDX-License-Identifier: MIT

package core

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Parsed is what Parse read from a line of free text.
type Parsed struct {
	// Date is the day the line names; zero means the reference date. To is the last day of a range (absences).
	Date Date `json:"date,omitempty"`
	To   Date `json:"to,omitempty"`
	// Start and End are clock times in minutes after midnight. End can pass midnight (up to Start + 24 h).
	Start *int `json:"start,omitempty"`
	End   *int `json:"end,omitempty"`
	// Minutes is the work time, or the signed deviation if Adjust is set.
	Minutes int  `json:"minutes"`
	Adjust  bool `json:"adjust,omitempty"`
	// Break is the break time.
	Break int `json:"break,omitempty"`
	// Absence is the absence type key, if the line is an absence.
	Absence string   `json:"absence,omitempty"`
	HalfDay bool     `json:"half_day,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	// Project is the path after @, such as "ACME/Website"; the caller resolves it.
	Project string `json:"project,omitempty"`
	Note    string `json:"note,omitempty"`
}

// ParseError says what in a line was not understood.
type ParseError struct {
	Token string
	Msg   string
}

func (e *ParseError) Error() string {
	if e.Token == "" {
		return e.Msg
	}
	return fmt.Sprintf("%q: %s", e.Token, e.Msg)
}

var (
	reSpan      = regexp.MustCompile(`^(\d{1,2})(?:[:.](\d{2}))?[-–](\d{1,2})(?:[:.](\d{2}))?$`)
	reHourMin   = regexp.MustCompile(`^([+-])?(\d{1,3}):(\d{2})$`)
	reDuration  = regexp.MustCompile(`^([+-])?(?:(\d{1,3}(?:[.,]\d{1,2})?)h)?(?:(\d{1,4})(?:m|min))?$`)
	reHoursMins = regexp.MustCompile(`^([+-])?(\d{1,3})h(\d{1,2})$`)
	reISODate   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reDotDate   = regexp.MustCompile(`^(\d{1,2})\.(\d{1,2})\.(\d{4})?$`)
)

var weekdays = map[string]time.Weekday{
	"mo": time.Monday, "mon": time.Monday, "montag": time.Monday, "monday": time.Monday,
	"di": time.Tuesday, "tue": time.Tuesday, "dienstag": time.Tuesday, "tuesday": time.Tuesday,
	"mi": time.Wednesday, "wed": time.Wednesday, "mittwoch": time.Wednesday, "wednesday": time.Wednesday,
	"do": time.Thursday, "thu": time.Thursday, "donnerstag": time.Thursday, "thursday": time.Thursday,
	"fr": time.Friday, "fri": time.Friday, "freitag": time.Friday, "friday": time.Friday,
	"sa": time.Saturday, "sat": time.Saturday, "samstag": time.Saturday, "saturday": time.Saturday,
	"so": time.Sunday, "sun": time.Sunday, "sonntag": time.Sunday, "sunday": time.Sunday,
}

var (
	relativeDays = map[string]int{"heute": 0, "today": 0, "gestern": -1, "yesterday": -1, "morgen": 1, "tomorrow": 1}
	absenceWords = map[string]string{
		"krank": AbsenceSick, "sick": AbsenceSick,
		"urlaub": AbsenceVacation, "vacation": AbsenceVacation, "leave": AbsenceVacation,
		"feiertag": AbsenceHoliday, "holiday": AbsenceHoliday,
		"zeitausgleich": AbsenceComp, "comp": AbsenceComp,
		"unbezahlt": AbsenceUnpaid, "unpaid": AbsenceUnpaid,
	}
	breakWords  = map[string]bool{"pause": true, "break": true}
	halfWords   = map[string]bool{"halb": true, "halber": true, "halbtag": true, "half": true, "½": true}
	earlyWords  = map[string]bool{"früher": true, "frueher": true, "early": true, "earlier": true}
	longerWords = map[string]bool{"länger": true, "laenger": true, "longer": true, "later": true}
)

// MaxInput is the longest line Parse accepts, in bytes.
const MaxInput = 500

type duration struct {
	minutes int
	signed  bool
	isBreak bool
}

// Parse reads one line such as
//
//	2h #meeting @ACME/Website Angebot schreiben
//	8-16:30 30m Pause
//	Fr -2h früher weg
//	Mo-Mi Urlaub
//	gestern 13:10-13:45 Telefonat #kunde
//
// A date is only read as the first word; a weekday means that day in the week of today (weeks start on Monday). Durations are 2h, 2.5h, 2,5h, 2h30m, 2h30,
// 90m, 90min and 2:30; a sign makes a deviation from the plan, as do the words früher/early (minus) and
// länger/longer (plus). Pause or break names the duration next to it as a break. Words that name an absence (krank,
// sick, Urlaub, vacation, Feiertag, holiday, Zeitausgleich, comp, unbezahlt, unpaid) make the line an absence; halb or
// half makes it a half day. #tag adds a tag, @path names the project, and every other word goes into the note.
func Parse(input string, today Date) (Parsed, error) {
	var p Parsed
	if !utf8.ValidString(input) {
		return p, &ParseError{Msg: "not valid UTF-8"}
	}
	if len(input) > MaxInput {
		return p, &ParseError{Msg: fmt.Sprintf("longer than %d bytes", MaxInput)}
	}
	tokens := strings.Fields(input)
	if len(tokens) == 0 {
		return p, &ParseError{Msg: "empty"}
	}
	var durations []duration
	var note []string
	breakPending, early, longer := false, false, false
	lastWasDuration := false

	// A date only counts as the first word: "so", "do" and "mi" are words, too.
	if from, to, ok, err := parseDateToken(strings.ToLower(tokens[0]), today); ok || err != nil {
		if err != nil {
			return p, &ParseError{tokens[0], err.Error()}
		}
		p.Date, p.To = from, to
		tokens = tokens[1:]
	}

	for _, tok := range tokens {
		word := strings.ToLower(strings.TrimRight(tok, ",;"))
		wasDuration := lastWasDuration
		lastWasDuration = false
		switch {
		case strings.HasPrefix(tok, "#") && len(tok) > 1:
			tag := strings.TrimRight(tok[1:], ",;")
			if !tagPattern.MatchString(tag) || len(tag) > 200 {
				return p, &ParseError{tok, "not a valid tag"}
			}
			if !containsString(p.Tags, tag) {
				p.Tags = append(p.Tags, tag)
			}
		case strings.HasPrefix(tok, "@") && len(tok) > 1:
			if p.Project != "" {
				return p, &ParseError{tok, "a second project"}
			}
			p.Project = strings.TrimRight(tok[1:], ",;")
			if p.Project == "" || len(p.Project) > 200 {
				return p, &ParseError{tok, "not a valid project"}
			}
		case breakWords[word]:
			if wasDuration && len(durations) > 0 && !durations[len(durations)-1].isBreak {
				durations[len(durations)-1].isBreak = true
			} else {
				breakPending = true
			}
		case halfWords[word]:
			p.HalfDay = true
		case absenceWords[word] != "":
			if p.Absence != "" && p.Absence != absenceWords[word] {
				return p, &ParseError{tok, "a second absence"}
			}
			p.Absence = absenceWords[word]
		default:
			if m := reSpan.FindStringSubmatch(word); m != nil {
				if p.Start != nil {
					return p, &ParseError{tok, "a second time span"}
				}
				start, err1 := clock(m[1], m[2])
				end, err2 := clock(m[3], m[4])
				if err1 != nil || err2 != nil {
					return p, &ParseError{tok, "not a valid time"}
				}
				if end == start {
					return p, &ParseError{tok, "start and end are the same"}
				}
				if end < start {
					end += 24 * 60
				}
				p.Start, p.End = &start, &end
				continue
			}
			if d, ok := parseDuration(word); ok {
				if d.minutes == 0 {
					return p, &ParseError{tok, "a duration of zero"}
				}
				d.isBreak = breakPending
				breakPending = false
				durations = append(durations, d)
				lastWasDuration = true
				continue
			}
			switch {
			case earlyWords[word]:
				early = true
			case longerWords[word]:
				longer = true
			}
			note = append(note, tok)
		}
	}
	p.Note = strings.Join(note, " ")

	if p.Absence != "" {
		if p.Start != nil || len(durations) > 0 {
			return p, &ParseError{Msg: "an absence takes no time; book the time separately"}
		}
		if p.Absence == AbsenceSick && p.Note != "" {
			return p, &ParseError{Msg: "no note for sickness: it would hold health data"}
		}
		if p.To.IsZero() {
			p.To = p.Date
		}
		return p, nil
	}
	if !p.To.IsZero() && p.To != p.Date {
		return p, &ParseError{Msg: "a range of days is only for absences"}
	}
	p.To = Date{}
	if p.HalfDay {
		return p, &ParseError{Msg: "half day is only for absences"}
	}

	var work []duration
	for _, d := range durations {
		if d.isBreak {
			if d.signed {
				return p, &ParseError{Msg: "a break has no sign"}
			}
			p.Break += d.minutes
			continue
		}
		work = append(work, d)
	}
	if len(work) > 1 {
		return p, &ParseError{Msg: "more than one duration"}
	}
	if len(work) == 1 {
		w := work[0]
		switch {
		case p.Start != nil:
			return p, &ParseError{Msg: "both a time span and a duration"}
		case early && longer:
			return p, &ParseError{Msg: "both earlier and longer"}
		case w.signed || early || longer:
			p.Adjust = true
			p.Minutes = w.minutes
			if !w.signed && early {
				p.Minutes = -w.minutes
			}
		default:
			p.Minutes = w.minutes
		}
	}
	// The limits of Entry.Validate: a deviation is at most a day, any other time at most a week.
	if (p.Adjust && (p.Minutes > 24*60 || p.Minutes < -24*60)) || p.Minutes > 7*24*60 || p.Break > 24*60 {
		return p, &ParseError{Msg: "the time is too long"}
	}
	if p.Start != nil {
		p.Minutes = *p.End - *p.Start
		if p.Break >= p.Minutes {
			return p, &ParseError{Msg: "the break is as long as the span"}
		}
	}
	if p.Start == nil && len(work) == 0 && p.Break == 0 {
		return p, &ParseError{Msg: "no time, span or absence found"}
	}
	return p, nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func clock(h, m string) (int, error) {
	hour, err := strconv.Atoi(h)
	if err != nil || hour > 24 {
		return 0, fmt.Errorf("hour")
	}
	minute := 0
	if m != "" {
		minute, err = strconv.Atoi(m)
		if err != nil || minute > 59 {
			return 0, fmt.Errorf("minute")
		}
	}
	if hour == 24 && minute != 0 {
		return 0, fmt.Errorf("after 24:00")
	}
	return hour*60 + minute, nil
}

func parseDuration(word string) (duration, bool) {
	var d duration
	if m := reHourMin.FindStringSubmatch(word); m != nil {
		h, _ := strconv.Atoi(m[2])
		mins, _ := strconv.Atoi(m[3])
		if mins > 59 {
			return d, false
		}
		d.minutes, d.signed = h*60+mins, m[1] != ""
		if m[1] == "-" {
			d.minutes = -d.minutes
		}
		return d, true
	}
	if m := reHoursMins.FindStringSubmatch(word); m != nil {
		h, _ := strconv.Atoi(m[2])
		mins, _ := strconv.Atoi(m[3])
		if mins > 59 {
			return d, false
		}
		d.minutes, d.signed = h*60+mins, m[1] != ""
		if m[1] == "-" {
			d.minutes = -d.minutes
		}
		return d, true
	}
	m := reDuration.FindStringSubmatch(word)
	if m == nil || (m[2] == "" && m[3] == "") {
		return d, false
	}
	total := 0.0
	if m[2] != "" {
		h, err := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", "."), 64)
		if err != nil {
			return d, false
		}
		total += h * 60
	}
	if m[3] != "" {
		mins, _ := strconv.Atoi(m[3])
		total += float64(mins)
	}
	d.minutes, d.signed = int(math.Round(total)), m[1] != ""
	if d.minutes > 7*24*60 {
		return d, false
	}
	if m[1] == "-" {
		d.minutes = -d.minutes
	}
	return d, true
}

// parseDateToken reads a date or a range of dates: heute/today, gestern/yesterday, morgen/tomorrow, a weekday (in the
// week of today), 2026-11-17, 17.11. or 17.11.2026, and two of them joined by a dash. ok reports whether the token
// looked like a date at all.
func parseDateToken(word string, today Date) (from, to Date, ok bool, err error) {
	if d, isDate, err := singleDate(word, today); isDate || err != nil {
		return d, d, isDate, err
	}
	// A range: split at a dash that is not part of an ISO date.
	for i := 1; i < len(word)-1; i++ {
		if word[i] != '-' {
			continue
		}
		a, okA, errA := singleDate(word[:i], today)
		b, okB, errB := singleDate(word[i+1:], today)
		if okA && okB {
			if errA != nil {
				return Date{}, Date{}, true, errA
			}
			if errB != nil {
				return Date{}, Date{}, true, errB
			}
			if b.Before(a) {
				return Date{}, Date{}, true, fmt.Errorf("the range ends before it starts")
			}
			if a.DaysUntil(b) > 366 {
				return Date{}, Date{}, true, fmt.Errorf("the range is longer than a year")
			}
			return a, b, true, nil
		}
	}
	return Date{}, Date{}, false, nil
}

func singleDate(word string, today Date) (Date, bool, error) {
	if n, ok := relativeDays[word]; ok {
		return today.AddDays(n), true, nil
	}
	if wd, ok := weekdays[strings.TrimSuffix(word, ".")]; ok {
		monday := today.StartOfWeek(time.Monday)
		return monday.AddDays((int(wd) + 6) % 7), true, nil
	}
	if reISODate.MatchString(word) {
		d, err := ParseDate(word)
		return d, true, err
	}
	if m := reDotDate.FindStringSubmatch(word); m != nil {
		day, _ := strconv.Atoi(m[1])
		month, _ := strconv.Atoi(m[2])
		year := today.Year
		if m[3] != "" {
			year, _ = strconv.Atoi(m[3])
		}
		d := NewDate(year, time.Month(month), day)
		if month < 1 || month > 12 || day < 1 || d.Day != day {
			return Date{}, true, fmt.Errorf("no such date")
		}
		return d, true, nil
	}
	return Date{}, false, nil
}

// EntryOptions say where parsed entries go.
type EntryOptions struct {
	Owner      string
	EmployerID string
	ProjectID  string
	Location   *time.Location
	// Today is the reference date for lines without a date.
	Today Date
}

// Entries turns a parsed line into entries: a work entry (a span with start and end, or a day entry), a break entry
// and a deviation (adjust). Spans are placed in the location, so a span over a daylight saving change keeps its real
// length. An absence gives no entries; use Absence.
func (p Parsed) Entries(o EntryOptions) ([]Entry, error) {
	if p.Absence != "" {
		return nil, invalid("an absence is not an entry")
	}
	loc := o.Location
	if loc == nil {
		var err error
		if loc, err = time.LoadLocation(DefaultTimeZone); err != nil {
			return nil, err
		}
	}
	date := p.Date
	if date.IsZero() {
		date = o.Today
	}
	base := Entry{Owner: o.Owner, EmployerID: o.EmployerID, ProjectID: o.ProjectID, Date: date, TimeZone: loc.String(),
		Tags: p.Tags, Note: p.Note, Source: SourceParse, Precision: PrecisionDay}
	var out []Entry
	switch {
	case p.Start != nil:
		e := base
		e.Kind = EntryWork
		start, end := date.At(*p.Start, loc), date.At(*p.End, loc)
		e.Start, e.End = &start, &end
		e.Precision = PrecisionSpan
		if err := e.Normalize(); err != nil {
			return nil, err
		}
		out = append(out, e)
	case p.Adjust:
		e := base
		e.Kind, e.Minutes = EntryAdjust, p.Minutes
		out = append(out, e)
	case p.Minutes > 0:
		e := base
		e.Kind, e.Minutes = EntryWork, p.Minutes
		out = append(out, e)
	}
	if p.Break > 0 {
		b := base
		b.Kind, b.Minutes, b.ProjectID, b.Tags, b.Note = EntryBreak, p.Break, "", nil, ""
		out = append(out, b)
	}
	return out, nil
}

// AbsenceRecord turns a parsed absence into an absence for the employer ("" for every employer).
func (p Parsed) AbsenceRecord(owner, employerID string, today Date) (Absence, error) {
	if p.Absence == "" {
		return Absence{}, invalid("not an absence")
	}
	from, to := p.Date, p.To
	if from.IsZero() {
		from, to = today, today
	}
	return Absence{Owner: owner, EmployerID: employerID, Type: p.Absence, From: from, To: to, HalfDay: p.HalfDay,
		Note: p.Note}, nil
}
