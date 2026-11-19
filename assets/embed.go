// SPDX-License-Identifier: MIT

// Package assets embeds the logo and its license note, so the program can show both. The logo is not under the MIT
// License; LICENSE-icon.md names its author, and the about dialog has to show that.
package assets

import _ "embed"

// Logo is the number eight in a yellow circle by Alfredo Creates from Flaticon, 512 by 512 pixels.
//
//go:embed logo.png
var Logo []byte

// IconLicense is the license note of the logo.
//
//go:embed LICENSE-icon.md
var IconLicense []byte
