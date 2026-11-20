# Backlog

<!-- GENERIERT von scripts/_backlog.py — nicht von Hand pflegen. Neu bauen: `python3 scripts/_backlog.py index` -->

Die Wahrheit sind die Einzeldateien in diesem Verzeichnis; diese Seite ist ihr Abzug.
Konventionen: [README-KONVENTION.md](README-KONVENTION.md).

## Meilensteine

* ☑ **[M-1](M-1-core-library.md)** The core library: model, calculations, parser, storage — noch keine Aufgaben
* ☑ **[M-2](M-2-standalone-binary.md)** The standalone program with a small web interface — noch keine Aufgaben
* ☑ **[M-3](M-3-embedding.md)** Embedding: another application runs the core in its own database — 5/5 erledigt
* ☐ **[M-4](M-4-first-release.md)** The first release — 0/1 erledigt
* ☑ **[M-5](M-5-list-view.md)** List view and leave planning: a start page that gives an overview — 6/6 erledigt
* ☑ **[M-6](M-6-views-and-detail.md)** Views and detail: one sheet in three views, typed in fewer keys — 5/5 erledigt

## Aufgaben

* ☑ **[T-1](T-1-approval.md)** Approval of periods and leave, leave reminders · M-3
* ☑ **[T-2](T-2-access-log.md)** Access log for the person concerned (A8) · M-3
* ☑ **[T-3](T-3-data-inventory.md)** Data inventory from the code (A21) and templates for employers (A18) · M-3
* ☑ **[T-4](T-4-loose-ends.md)** Loose ends: first link, region names, reviews, reads once a day, reminders, legal templates, leave notices · M-3
* ☐ **[T-5](T-5-container-image.md)** Publish the container image · M-4
* ☑ **[T-6](T-6-list-and-codes.md)** The list as the start page: hours per row, short codes, running balance, corrections · M-5
* ☑ **[T-7](T-7-projects-report-employers.md)** Projects per day and the printable weekly report · M-5
* ☑ **[T-8](T-8-leave-planning.md)** Leave planning in the year's grid · M-5
* ☑ **[T-9](T-9-endless-list-and-change-requests.md)** The whole period as an endless list; change requests on approved weeks · M-5
* ☑ **[T-10](T-10-employers-in-one-list.md)** Several employers in one list, with their pay, extras, kilometres and standby · M-5
* ☑ **[T-11](T-11-embedding-hooks.md)** Hooks for an embedding application: mount path, its sign-in, its tenant, a frame, a calendar · M-3
* ☑ **[T-12](T-12-half-days-and-standby-pay.md)** Pay for half days, paid standby, one currency · M-5
* ☑ **[T-13](T-13-sheet-view-model.md)** The sheet: one view model, three views, the endless list from today · M-6
* ☑ **[T-14](T-14-line-of-a-day.md)** The line of a day: the core's parser for one day, employer keys, jobs by a word · M-6
* ☑ **[T-15](T-15-preferences.md)** Preferences per person: the view, the detail, the employers whose jobs stand open · M-6
* ☑ **[T-16](T-16-clean-up-list-and-week.md)** Clean-up: the former list and /weeks go, the week page keeps its tools · M-6
* ☑ **[T-17](T-17-m6-follow-ups.md)** M-6 follow-ups: notes and projects of an approved week as change requests, typed spans for the rest · M-6

## Entscheidungen (ADR)

* ☑ **[ADR-1](ADR-1-library-and-binary.md)** A Go library at the core, and a standalone binary on top
* ☑ **[ADR-2](ADR-2-data-model.md)** One entry table, modes as views, everything with a valid-from date
* ☑ **[ADR-3](ADR-3-half-days-and-standby.md)** Half days on the work, standby as pay rules, one currency
* ☑ **[ADR-4](ADR-4-views-of-one-view-model.md)** Three views as renderers of one view model, preferences per person
