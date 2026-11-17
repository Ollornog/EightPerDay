# Backlog

<!-- GENERIERT von scripts/_backlog.py — nicht von Hand pflegen. Neu bauen: `python3 scripts/_backlog.py index` -->

Die Wahrheit sind die Einzeldateien in diesem Verzeichnis; diese Seite ist ihr Abzug.
Konventionen: [README-KONVENTION.md](README-KONVENTION.md).

## Meilensteine

* ☑ **[M-1](M-1-core-library.md)** The core library: model, calculations, parser, storage — noch keine Aufgaben
* ☑ **[M-2](M-2-standalone-binary.md)** The standalone program with a small web interface — noch keine Aufgaben
* ☐ **[M-3](M-3-embedding.md)** Embedding: another application runs the core in its own database — 4/4 erledigt
* ☐ **[M-4](M-4-first-release.md)** The first release — 0/1 erledigt

## Aufgaben

* ☑ **[T-1](T-1-approval.md)** Approval of periods and leave, leave reminders · M-3
* ☑ **[T-2](T-2-access-log.md)** Access log for the person concerned (A8) · M-3
* ☑ **[T-3](T-3-data-inventory.md)** Data inventory from the code (A21) and templates for employers (A18) · M-3
* ☑ **[T-4](T-4-loose-ends.md)** Loose ends: first link, region names, reviews, reads once a day, reminders, legal templates · M-3
* ☐ **[T-5](T-5-container-image.md)** Publish the container image · M-4

## Entscheidungen (ADR)

* ☑ **[ADR-1](ADR-1-library-and-binary.md)** A Go library at the core, and a standalone binary on top
* ☑ **[ADR-2](ADR-2-data-model.md)** One entry table, modes as views, everything with a valid-from date
