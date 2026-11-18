// SPDX-License-Identifier: MIT

package core

import "strings"

// DayCode is what a short code typed into a day stands for: an absence type, for the whole day or half of it.
type DayCode struct {
	// Absence is the key of the built-in absence type.
	Absence string `json:"absence"`
	HalfDay bool   `json:"half_day,omitempty"`
}

// dayCodes maps the short codes of German and English spreadsheets to the built-in absence types. Public holidays
// need no code: they come from the employer's region.
var dayCodes = map[string]string{
	"U": AbsenceVacation, "V": AbsenceVacation, // Urlaub, vacation
	"K": AbsenceSick, "S": AbsenceSick, // krank, sick
	"F": AbsenceHoliday, "O": AbsenceHoliday, // frei, day off
	"ZA": AbsenceComp, "C": AbsenceComp, // Zeitausgleich, compensation (time off in lieu)
}

// halfMarks are the ways a half day is written in front of or behind a code: ½U, U½, U/2, 0,5U, ,5U.
var halfMarks = []string{"½", "/2", "0,5", "0.5", ",5", ".5"}

// ParseDayCode reads a short code such as U, K, F, ZA, ½U or U/2, in upper or lower case, as an absence type; spaces
// may stand around the code and between the code and its half. ok is false when the input is no code, for example
// hours.
func ParseDayCode(input string) (DayCode, bool) {
	s := strings.ToUpper(strings.TrimSpace(input))
	half := false
	for _, mark := range halfMarks {
		if rest, found := strings.CutPrefix(s, mark); found {
			s, half = strings.TrimSpace(rest), true
			break
		}
		if rest, found := strings.CutSuffix(s, mark); found {
			s, half = strings.TrimSpace(rest), true
			break
		}
	}
	key, ok := dayCodes[s]
	if !ok {
		return DayCode{}, false
	}
	return DayCode{Absence: key, HalfDay: half}, true
}

// CodedAbsence reports whether an absence type has a short code, so a day's row may show and replace it.
func CodedAbsence(key string) bool {
	for _, k := range dayCodes {
		if k == key {
			return true
		}
	}
	return false
}
