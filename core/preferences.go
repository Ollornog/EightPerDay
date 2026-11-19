// SPDX-License-Identifier: MIT

package core

// Preferences are how a person likes the interface to show the records: the view of the start page, how much detail
// it shows, and the employers whose projects stand open next to them. One record per person, its ID is the person's
// (the kind keeps it apart from other records). The library only keeps the words; the interface gives them meaning
// (see web: ADR-4).
type Preferences struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	// View is the view of the start page, such as "table", "journal" or "grid"; empty lets the interface choose.
	View string `json:"view,omitempty"`
	// Detail is how much the start page shows, such as "auto", "hours", "projects" or "employers"; empty is "auto".
	Detail string `json:"detail,omitempty"`
	// Expanded are the employers whose projects stand open next to them.
	Expanded []string `json:"expanded,omitempty"`
}

// RecPreferences is the kind of the preferences of a person.
const RecPreferences Kind = "preferences"

// Meta files the preferences; they belong to no employer.
func (p Preferences) Meta() Meta { return Meta{RecPreferences, p.ID, p.Owner, ""} }

// Validate checks the preferences.
func (p Preferences) Validate() error {
	if err := checkKeys(p.ID, p.Owner); err != nil {
		return err
	}
	if p.ID != p.Owner {
		return invalid("the preferences of a person carry the person's ID")
	}
	if len(p.View) > 20 || len(p.Detail) > 20 {
		return invalid("view or detail longer than 20 characters")
	}
	if len(p.Expanded) > 100 {
		return invalid("more than 100 open employers")
	}
	for _, id := range p.Expanded {
		if err := checkRef("employer", id, true); err != nil {
			return err
		}
	}
	return nil
}

// PreferencesOf returns the person's preferences from the catalog, or empty ones with the person's ID.
func (c Catalog) PreferencesOf(owner string) Preferences {
	for _, p := range c.Preferences {
		if p.Owner == owner {
			return p
		}
	}
	return Preferences{ID: owner, Owner: owner}
}
