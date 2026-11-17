// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/Ollornog/EightPerDay/core"
)

// Every region of public holidays has a name in every language, the parts of a state included, and the list puts
// Germany first, each state before its parts, and Austria after Germany.
func TestRegionsHaveNames(t *testing.T) {
	c, err := loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range languages {
		options := c.regionOptions(lang)
		if len(options) != len(core.Regions()) {
			t.Fatalf("%s: %d options for %d regions", lang, len(options), len(core.Regions()))
		}
		var codes []string
		for _, o := range options {
			codes = append(codes, o.Code)
			if o.Name == "" || o.Name == o.Code || strings.HasPrefix(o.Name, "!") {
				t.Errorf("%s: region %s has no name: %q", lang, o.Code, o.Name)
			}
		}
		if codes[0] != "DE" || codes[len(codes)-1] != "AT" {
			t.Errorf("%s: Germany first and Austria last: %v", lang, codes)
		}
		for _, part := range []string{"DE-BY-AUGSBURG", "DE-BY-PROTESTANT", "DE-SN-CATHOLIC", "DE-TH-CATHOLIC"} {
			state := part[:5]
			i, j := slices.Index(codes, state), slices.Index(codes, part)
			if i < 0 || j <= i {
				t.Errorf("%s: %s does not follow %s: %v", lang, part, state, codes)
			}
			for _, between := range codes[i+1 : j] {
				if !strings.HasPrefix(between, state+"-") {
					t.Errorf("%s: %s stands between %s and its part %s", lang, between, state, part)
				}
			}
		}
	}
	if got := c.regionOptions("de"); !slices.ContainsFunc(got, func(o regionOption) bool {
		return o.Code == "DE-BY-AUGSBURG" && o.Name == "Bayern: Stadt Augsburg (mit Friedensfest)"
	}) {
		t.Errorf("the German name of Augsburg is missing: %v", got)
	}
}

// The selects of a region show the names and keep the codes as values, also for the region already chosen.
func TestRegionSelectShowsNames(t *testing.T) {
	s, _ := trackSite(t)
	boss := s.me("boss", true)
	s.mustPost("/orgs", url.Values{"name": {"ACME"}, "region": {"DE-TH-CATHOLIC"}, "back": {"/orgs"}})
	memberships, _ := s.store.Memberships(context.Background(), "", boss.ID)
	org := memberships[0].Organization
	s.post("/language", url.Values{"lang": {"de"}, "back": {"/"}})
	page := s.get("/orgs/" + org).body
	if !strings.Contains(page, `<option value="DE-TH-CATHOLIC" selected>Thüringen: Gemeinden mit Fronleichnam (überwiegend katholisch)</option>`) {
		t.Error("the organization's region is not shown by its name")
	}
	if !strings.Contains(s.get("/orgs").body, `<option value="DE-BY">Bayern</option>`) {
		t.Error("the form for a new organization shows no names")
	}
	if !strings.Contains(s.get("/").body, `<option value="AT">Österreich</option>`) {
		t.Error("the form for a new employer shows no names")
	}
}
