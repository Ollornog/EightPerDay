// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

// The page of the templates says first that they are no legal advice, shows every template of docs/legal and the
// kinds of data, and hands out the files exactly as they are in the repository.
func TestLegalPage(t *testing.T) {
	s := newSite(t)
	page := s.get("/legal")
	if page.status != http.StatusOK {
		t.Fatalf("%d", page.status)
	}
	disclaimer := strings.Index(page.body, "data-legal-disclaimer")
	if disclaimer < 0 || !strings.Contains(page.body, "No legal advice, not checked by a lawyer") ||
		disclaimer > strings.Index(page.body, "data-legal-file") {
		t.Fatal("the page does not say first that the templates are no legal advice")
	}
	for _, f := range legalFiles {
		want, err := os.ReadFile("../docs/legal/" + f.name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(page.body, `data-legal-file="`+f.name+`"`) || !strings.Contains(page.body, strings.TrimSpace(string(want))) {
			t.Errorf("%s is not shown whole", f.name)
		}
		a := s.get("/legal/" + f.name)
		if a.status != http.StatusOK || a.body != string(want) || !strings.HasPrefix(a.header.Get("Content-Type"), "text/markdown") ||
			!strings.Contains(a.header.Get("Content-Disposition"), "attachment") {
			t.Errorf("download of %s: %d %q %q", f.name, a.status, a.header.Get("Content-Type"), a.header.Get("Content-Disposition"))
		}
	}
	inventory, err := os.ReadFile("../docs/data-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	if a := s.get("/legal/data-inventory.json"); a.status != http.StatusOK || a.body != string(inventory) ||
		!strings.HasPrefix(a.header.Get("Content-Type"), "application/json") {
		t.Errorf("download of the inventory: %d %q", a.status, a.header.Get("Content-Type"))
	}
	if !strings.Contains(page.body, `data-kind="Employer"`) || !strings.Contains(page.body, `data-kind="Access"`) {
		t.Error("the kinds of data are not listed")
	}
	for _, bad := range []string{"/legal/nothing.md", "/legal/docs.go", "/legal/..%2Fdocs.go", "/legal/README.txt"} {
		if a := s.get(bad); a.status != http.StatusNotFound {
			t.Errorf("%s: %d", bad, a.status)
		}
	}
	s.post("/language", url.Values{"lang": {"de"}, "back": {"/legal"}})
	if body := s.get("/legal").body; !strings.Contains(body, "Keine Rechtsberatung, nicht juristisch geprüft") {
		t.Error("the German page lacks the disclaimer")
	}
}
