// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
)

// ExportFormat is the version of the export layout.
const ExportFormat = 1

// Export is everything stored about one person, for the right of access and for taking the data along: the catalog,
// every entry and absence including deleted ones, and the full correction history.
type Export struct {
	Format     int        `json:"format"`
	ExportedAt time.Time  `json:"exported_at"`
	Owner      string     `json:"owner"`
	Catalog    Catalog    `json:"catalog"`
	Entries    []Entry    `json:"entries"`
	Absences   []Absence  `json:"absences"`
	Revisions  []Revision `json:"revisions"`
	// Accesses is the person's access log, if the store keeps one (AccessLog).
	Accesses []Access `json:"accesses,omitempty"`
}

// ExportOwner collects the export of the owner from the store, with the access log if the store keeps one.
func ExportOwner(ctx context.Context, s Store, owner string) (Export, error) {
	cat, err := s.Catalog(ctx, owner)
	if err != nil {
		return Export{}, err
	}
	entries, err := s.Entries(ctx, EntryFilter{Owner: owner, IncludeDeleted: true})
	if err != nil {
		return Export{}, err
	}
	absences, err := s.Absences(ctx, AbsenceFilter{Owner: owner, IncludeDeleted: true})
	if err != nil {
		return Export{}, err
	}
	revs, err := s.Revisions(ctx, owner, "")
	if err != nil {
		return Export{}, err
	}
	x := Export{Format: ExportFormat, ExportedAt: time.Now().UTC(), Owner: owner, Catalog: cat, Entries: entries,
		Absences: absences, Revisions: revs}
	if log, ok := s.(AccessLog); ok {
		if x.Accesses, err = log.Accesses(ctx, owner); err != nil {
			return Export{}, err
		}
	}
	return x, nil
}

// WriteJSON writes the export as indented JSON.
func (x Export) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(x)
}

// EntryColumns are the columns of WriteEntriesCSV.
var EntryColumns = []string{"id", "employer_id", "date", "kind", "start", "end", "minutes", "precision", "project_id",
	"activity_id", "tags", "note", "source", "rate", "currency", "version", "deleted_at"}

// WriteEntriesCSV writes entries as CSV with a header line (EntryColumns). Times are RFC 3339 in UTC, tags are joined
// by spaces, the rate is in minor units. Cells that a spreadsheet would run as a formula are prefixed with a quote.
func WriteEntriesCSV(w io.Writer, entries []Entry) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(EntryColumns); err != nil {
		return err
	}
	stamp := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	for _, e := range entries {
		rate, currency := "", ""
		if e.Rate != nil {
			rate, currency = strconv.FormatInt(e.Rate.Amount, 10), e.Rate.Currency
		}
		row := []string{e.ID, e.EmployerID, e.Date.String(), string(e.Kind), stamp(e.Start), stamp(e.End),
			strconv.Itoa(e.Minutes), string(e.Precision), e.ProjectID, e.ActivityID, strings.Join(e.Tags, " "),
			safeCell(e.Note), string(e.Source), rate, currency, strconv.Itoa(e.Version), stamp(e.DeletedAt)}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// safeCell defuses CSV injection: a cell that starts like a formula gets a leading quote.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
