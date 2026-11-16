# Mitwirken

<a href="../CONTRIBUTING.md">English</a> · <b>Deutsch</b>
<br /><br />

Danke, dass du dir die Zeit nimmst. Das Projekt ist klein, deshalb sind die Regeln kurz.

## Bevor du Code schreibst

**Erst ein Issue eröffnen** für alles, was über einen Tippfehler oder einen offensichtlichen Fehler hinausgeht. Die
Begründungen hinter dem Entwurf stehen in [`backlog/`](../backlog/) als Entscheidungsprotokolle; bitte das passende
lesen, bevor du vorschlägst, es zu ändern.

**Eigener Code.** EightPerDay teilt Ziele mit anderen Zeiterfassungen, nicht deren Code. Bitte keinen Code, keine Texte,
Grafiken oder Übersetzungen aus ihnen kopieren oder übertragen, besonders nicht aus Projekten unter AGPL, GPL oder SSPL.

## Ablauf

- Arbeite auf einem **Feature-Branch**, nicht auf `main`.
- Vor dem Push die volle Suite fahren: `scripts/check.sh`. Sie muss **zweimal hintereinander** grün sein. Sie braucht
  Go, Python 3 und die PostgreSQL-Server-Programme (`initdb`, `pg_ctl`, `postgres`; gefunden über `PG_BIN` oder unter
  `/usr/lib/postgresql/<version>/bin`).
- Doku und `CHANGELOG.md` gehören in **denselben Commit** wie die Änderung, die sie beschreiben.
- Code, Kommentare, Commits und Doku sind Englisch. Deutsch gibt es nur als Übersetzung.
- Beispiele nutzen `example.com` und erfundene Namen, nie echte Firmen, Hosts oder Daten.

## Was die Suite prüft

Neben den Go-Tests erzwingt die Suite Repository-Hygiene: Pflichtdateien, gepinnte GitHub Actions,
Workflow-Berechtigungen, Changelog-Aufbau und dass deutsche und englische Dokumente dieselbe Gestalt behalten. Schlägt
die Hygiene an, die Ursache beheben, statt die Prüfung zu umgehen.

## Berechnungen

Jede Regel, die eine Zahl berechnet (Soll, Ist, Saldo, Urlaub, Verdienst), braucht einen Tabellentest, auch für
Monatsenden, den Wechsel von und zur Sommerzeit und ein Zeitmodell, das sich mitten im Monat ändert. Eine Korrektur
kommt mit einem Test, der ohne sie fehlschlägt.

## Verhaltenskodex

Mit deiner Teilnahme stimmst du dem [Verhaltenskodex](CODE_OF_CONDUCT.de.md) zu.

<br /><br />
<p align="right"><img src="../assets/logo.png" alt="EightPerDay" width="60" height="60"></p>
