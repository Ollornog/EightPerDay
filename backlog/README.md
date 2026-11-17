# Backlog

<!-- GENERIERT von scripts/_backlog.py — nicht von Hand pflegen. Neu bauen: `python3 scripts/_backlog.py index` -->

Die Wahrheit sind die Einzeldateien in diesem Verzeichnis; diese Seite ist ihr Abzug.
Konventionen: [README-KONVENTION.md](README-KONVENTION.md).

## Meilensteine

* ☑ **[M-1](M-1-core-library.md)** The core library: model, calculations, parser, storage — noch keine Aufgaben
* ☐ **[M-2](M-2-standalone-binary.md)** The standalone program with a small web interface — noch keine Aufgaben
* ☐ **[M-3](M-3-embedding.md)** Embedding: another application runs the core in its own database — noch keine Aufgaben

## Entscheidungen (ADR)

* ☑ **[ADR-1](ADR-1-library-and-binary.md)** A Go library at the core, and a standalone binary on top
* ☑ **[ADR-2](ADR-2-data-model.md)** One entry table, modes as views, everything with a valid-from date
