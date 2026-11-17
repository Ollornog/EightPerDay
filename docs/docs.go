// SPDX-License-Identifier: MIT

// Package docs carries the templates for employers into the binary, so the program shows them and offers them for
// download (the page /legal): the texts of legal/ and the data inventory, which a test in core keeps in step with the
// code.
package docs

import "embed"

// Files holds legal/*.md and data-inventory.json.
//
//go:embed legal/*.md data-inventory.json
var Files embed.FS
