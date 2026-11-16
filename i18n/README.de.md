<p align="center"><img src="../assets/logo.png" width="250" height="250" alt="EightPerDay-Logo, eine Stoppuhr"></p>
<h1 align="center">EightPerDay</h1>

<p align="center"><a href="../README.md">English</a> · <b>Deutsch</b></p>

**Zeiterfassung, die sich nach deiner Arbeit richtet, nicht umgekehrt.**

> **Stand: früh.** Das Repository ist gerade angelegt. Die Kernbibliothek entsteht, siehe [`backlog/`](../backlog/).

## Warum

Die meisten Zeiterfassungen wollen jeden Arbeitstag von Beginn bis Ende gebucht haben. Die quelloffenen kennen Soll,
Überstunden oder Urlaub kaum, und keine kommt mit mehreren Arbeit- oder Auftraggebern mit eigenem Zeitmodell und eigener
Vergütung zurecht. Wer als Handwerker nur „Mittwoch krank“ notieren will oder als Freiberufler drei Kunden hat, landet
bei einer Tabelle.

## Was es können wird

- **Mehrere Arbeit- und Auftraggeber je Person**, auch private, jeder mit eigenem Zeitmodell und eigener Vergütung.
- **Betriebsarten je Arbeitgeber, jederzeit wechselbar ohne Datenverlust:**
  - *Wochensumme*: eine Zahl je Woche, die nächste Woche beginnt leer.
  - *Vorlage*: Soll je Wochentag, Saldo und Überstunden.
  - *Plan gilt*: die Vorlage gilt als gearbeitet, gebucht wird nur, was abweicht (krank, Urlaub, länger, früher weg),
    und die Woche wird bestätigt.
  - *Genau*: Beginn, Ende und Pausen, aufgeteilt auf Projekte.
  - *Nur Projekte*: Zeit je Projekt, keine Gesamtarbeitszeit.
- **Zeitmodelle und Vergütungsregeln mit Gültig-ab-Datum**, damit eine Änderung nie die Vergangenheit umschreibt.
- **Vergütung je Stunde, je Projekt, je Einsatz oder je Zeitraum**, Verdienst je Arbeitgeber und Zeitraum.
- **Abwesenheiten** (Urlaub, Krankheit, Feiertage und mehr), Feiertage der deutschen Bundesländer und Österreichs als
  Daten.
- **Freitext-Eingabe** wie `2h #meeting @Website ACME` oder `8-16:30 30m Pause`.
- **Korrekturhistorie statt Überschreiben**, einstellbare Aufbewahrung und ein Export für die betroffene Person.

## Wie es gebaut ist

- Eine **Go-Bibliothek** (`core`) mit Datenmodell, Berechnungen, Parser und einer Speicher-Schnittstelle, mit einer
  PostgreSQL-Umsetzung (eingebettete Migrationen, Tabellenpräfix, Mandanten-Spalte mit Row-Level-Security) und einer
  im Speicher.
- Ein **eigenständiges Programm** mit schlanker Weboberfläche darauf.
- Die Bibliothek ist zum Einbinden gedacht: Eine andere Anwendung kann die Migrationen in ihrer eigenen Datenbank
  fahren und eine eigene Oberfläche bauen. [UnderDoc](https://github.com/Ollornog/UnderDoc) tut das.

Entscheidungen stehen in [`backlog/`](../backlog/).

## Entwicklung

`scripts/check.sh` fährt alles: Repository-Hygiene, gofmt, go vet, staticcheck, govulncheck und die Tests mit dem
Race-Detektor. Es braucht Go, Python 3 und die PostgreSQL-Server-Programme. Siehe
[CONTRIBUTING](CONTRIBUTING.de.md).

## Lizenz

Der Code steht unter der [MIT-Lizenz](../LICENSE).

Das Logo steht **nicht** unter der MIT-Lizenz: Icon von [juicy_fish](https://www.flaticon.com/de/autoren/juicy-fish) bei
[Flaticon](https://www.flaticon.com/de/), genutzt unter der Flaticon-Lizenz mit Namensnennung, siehe
[assets/LICENSE-icon.md](../assets/LICENSE-icon.md).
