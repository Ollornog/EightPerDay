// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"slices"
	"testing"
)

var updateInventory = flag.Bool("update-inventory", false, "write docs/data-inventory.json from the code")

const inventoryFile = "../docs/data-inventory.json"

// The inventory describes every field of every stored record, and the file in docs/ is the inventory of the code.
// After a change of the model: go test ./core -run TestDataInventory -update-inventory
func TestDataInventory(t *testing.T) {
	inv := DataInventory()
	var kinds []Kind
	for _, k := range inv {
		if k.Purpose == "" || k.Holder == "" || k.Deletion == "" {
			t.Errorf("%s: purpose, holder and deletion must be said", k.Name)
		}
		for _, f := range k.Fields {
			if f.Purpose == "" {
				t.Errorf("%s.%s has no description: add one to the inventory in inventory.go", k.Name, f.Name)
			}
		}
		if k.Class != "" {
			if err := (Retention{Class: k.Class, YearsAfterYearEnd: MinimumYears}).Validate(); err != nil {
				t.Errorf("%s: %v", k.Name, err)
			}
		}
		if k.Kind != "" {
			kinds = append(kinds, k.Kind)
		}
	}
	for _, kind := range Kinds {
		if !slices.Contains(kinds, kind) {
			t.Errorf("catalog kind %s is missing from the inventory", kind)
		}
	}
	// No description for a field that no longer exists.
	for _, x := range inventory {
		fields := jsonFields(x.value)
		for name := range x.fields {
			if !slices.Contains(fields, name) {
				t.Errorf("%s: description of a field %q it does not have", reflect.TypeOf(x.value).Name(), name)
			}
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{
		"about": "The data EightPerDay stores, field by field, generated from the code (core.DataInventory). " +
			"An aid for the record of processing activities and the information of the people concerned; no legal advice.",
		"records": inv,
	}); err != nil {
		t.Fatal(err)
	}
	if *updateInventory {
		if err := os.WriteFile(inventoryFile, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(inventoryFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, buf.Bytes()) {
		t.Fatalf("%s is out of date: go test ./core -run TestDataInventory -update-inventory", inventoryFile)
	}
}
