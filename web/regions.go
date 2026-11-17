// SPDX-License-Identifier: MIT

package web

import (
	"slices"
	"strings"

	"github.com/Ollornog/EightPerDay/core"
)

// regionOption is a region of public holidays as a select shows it: its code and a name a person can read.
type regionOption struct {
	Code string
	Name string
}

// regionOptions lists the regions of core.Regions with their names in the language: Germany first, then its states by
// name, each followed by its parts (such as the city of Augsburg), then Austria, then any region an embedding added by
// core.AddHolidayRule, under its code unless the catalog names it.
func (c catalog) regionOptions(lang string) []regionOption {
	texts := c[lang]
	name := func(code string) string {
		if text, ok := texts["region."+code]; ok {
			return text
		}
		return code
	}
	type sorted struct {
		regionOption
		group int    // 0 Germany, 1 Austria, 2 anything else
		key   string // the state's name, then the part's
	}
	var all []sorted
	for _, code := range core.Regions() {
		s := sorted{regionOption: regionOption{Code: code, Name: name(code)}, group: 2, key: code}
		parts := strings.SplitN(code, "-", 3)
		switch {
		case code == "DE":
			s.group, s.key = 0, ""
		case parts[0] == "DE" && len(parts) == 2:
			s.group, s.key = 0, name(code)
		case parts[0] == "DE" && len(parts) == 3:
			s.group, s.key = 0, name(parts[0]+"-"+parts[1])+"\x00"+s.Name
		case code == "AT":
			s.group, s.key = 1, ""
		}
		all = append(all, s)
	}
	slices.SortStableFunc(all, func(a, b sorted) int {
		if a.group != b.group {
			return a.group - b.group
		}
		return strings.Compare(a.key, b.key)
	})
	out := make([]regionOption, len(all))
	for i, s := range all {
		out[i] = s.regionOption
	}
	return out
}
