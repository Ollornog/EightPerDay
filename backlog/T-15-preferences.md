---
id: T-15
type: Task
title: "Preferences per person: the view, the detail, the employers whose jobs stand open"
status: erledigt
milestone: M-6
tags: [core, ui]
created: 2026-11-19
---

- `core.Preferences` (catalog kind `preferences`, one record per person, ID = the person) keeps the view, the detail
  and the employers whose projects stand open; it is exported and imported with the private part and erased with it.
- The menu "View" of the start page and the button next to an employer save it (`POST /sheet/prefs`). Without a choice
  the table opens, on a narrow screen the journal (`Sec-CH-UA-Mobile`, else the script by the width of the screen).

Done when a test proves that view and detail are kept for one person and reach no other.
