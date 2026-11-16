// SPDX-License-Identifier: MIT

// Package core is the time tracking library of EightPerDay: the data model, the calculations (target, actual,
// balance, overtime, leave, earnings), the free-text parser and the storage interface. It knows nothing about HTTP or
// sign-in; an application embeds it and brings both. See backlog/ADR-1 and backlog/ADR-2.
package core
