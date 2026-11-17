// SPDX-License-Identifier: MIT

package web

import (
	"io/fs"
	"net/http"

	"github.com/Ollornog/EightPerDay/core"
	"github.com/Ollornog/EightPerDay/docs"
)

// The templates for employers (docs/legal) and the data inventory, shown and offered for download. They are drafts
// from the software's side: no legal advice and not checked by a lawyer, which the page says first.

// legalFile is one template as the page shows it.
type legalFile struct {
	Name  string // the file name, also the address of its download
	Key   string // the catalog key of its title and description
	Text  string
	Bytes int
}

type legalData struct {
	Files     []legalFile
	Inventory []core.DataKind
}

// legalFiles are the templates in the order the page shows them; README.md is the overview.
var legalFiles = []struct{ name, key string }{
	{"README.md", "legal.file.readme"},
	{"works-agreement.md", "legal.file.agreement"},
	{"processing-record.md", "legal.file.record"},
}

// legalPage shows the templates for employers and the kinds of data the program stores.
func (h *handler) legalPage(w http.ResponseWriter, r *http.Request) {
	d := legalData{Inventory: core.DataInventory()}
	for _, f := range legalFiles {
		text, err := fs.ReadFile(docs.Files, "legal/"+f.name)
		if err != nil {
			h.failed(w, r, err)
			return
		}
		d.Files = append(d.Files, legalFile{Name: f.name, Key: f.key, Text: string(text), Bytes: len(text)})
	}
	h.render(w, r, http.StatusOK, "legal", view{Title: h.t(r, "legal.title"), Data: d})
}

// legalDownload hands out one template or the data inventory as a file.
func (h *handler) legalDownload(w http.ResponseWriter, r *http.Request) {
	// Only what docs.Files holds can come out: legal/*.md and the inventory; fs.ReadFile refuses "..".
	name := r.PathValue("name")
	file, contentType := "legal/"+name, "text/markdown; charset=utf-8"
	if name == "data-inventory.json" {
		file, contentType = name, "application/json; charset=utf-8"
	}
	body, err := fs.ReadFile(docs.Files, file)
	if err != nil {
		h.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="eightperday-`+name+`"`)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}
