<p align="center"><img src="../assets/logo.png" width="250" height="250" alt="EightPerDay-Logo, eine Stoppuhr"></p>
<h1 align="center">EightPerDay</h1>

<p align="center"><a href="../README.md">English</a> · <b>Deutsch</b></p>

**Zeiterfassung, die sich nach deiner Arbeit richtet, nicht umgekehrt.**

> **Stand: früh.** Die Kernbibliothek steht ([docs/core-api.md](../docs/core-api.md), Englisch); das eigenständige
> Programm mit Weboberfläche entsteht, siehe [`backlog/`](../backlog/).

## Warum

Die meisten Zeiterfassungen wollen jeden Arbeitstag von Beginn bis Ende gebucht haben. Die quelloffenen kennen Soll,
Überstunden oder Urlaub kaum, und keine kommt mit mehreren Arbeit- oder Auftraggebern mit eigenem Zeitmodell und eigener
Vergütung zurecht. Wer als Handwerker nur „Mittwoch krank“ notieren will oder als Freiberufler drei Kunden hat, landet
bei einer Tabelle.

## Was es können wird

- **Mehrere Arbeit- und Auftraggeber je Person**, auch private, jeder mit eigenem Zeitmodell und eigener Vergütung.
- **Organisationen**, die die Zeiterfassung ihrer Mitglieder verwalten, streng getrennt von dem, was eine Person privat
  erfasst; beides geht gleichzeitig, mit einer Gesamtsicht über alles.
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

## Betrieb

Ein Programm, PostgreSQL, sonst nichts; die Weboberfläche ist fürs Handy zuerst gebaut und lässt sich auf den
Startbildschirm legen. [`compose.yaml`](../compose.yaml) ist ein Beispiel mit Datenbank. Die Einstellungen kommen aus der
Umgebung:

| Variable | Bedeutung |
|---|---|
| `EIGHTPERDAY_DATABASE_URL` (oder `…_FILE`) | Verbindung zu PostgreSQL, Pflicht |
| `EIGHTPERDAY_ORIGIN` | die Adresse, die man öffnet, etwa `https://example.org`; Passkeys hängen an ihrem Hostnamen |
| `EIGHTPERDAY_ADDR`, `EIGHTPERDAY_HEALTH_ADDR` | wo Oberfläche und Gesundheitsprüfung lauschen (`127.0.0.1:8080`, `127.0.0.1:8081`) |
| `EIGHTPERDAY_TRUSTED_PROXIES` | Netze, deren `X-Forwarded-For` und Gateway-Kopfzeilen geglaubt werden |
| `EIGHTPERDAY_GATE_USER_HEADER` und `…_NAME_HEADER`, `…_GROUPS_HEADER`, `…_ADMIN_GROUP` | wahlweise: einem Anmelde-Gateway davor vertrauen |

**Anmeldung.** Es gibt keine Selbstregistrierung. Beim ersten Start schreibt das Programm einen Einmal-Link für das erste
Konto, das das Programm verwaltet; `eightperday link [--admin] [<name>]` erzeugt an der Konsole eine Einladung, und
Verwaltende laden in der Oberfläche ein. Jede Person wählt einen Passkey (die Vorgabe), ein Passwort mit
Authenticator-App oder nur ein Passwort. Passwörter werden mit Argon2id gehasht, Fehlversuche je Name und je Adresse
gebremst, Sitzungen liegen in HTTP-only-Cookies. Hinter einem Anmelde-Gateway können dessen Kopfzeilen anmelden, aber
nur von den vertrauten Proxys.

## Entwicklung

`scripts/check.sh` fährt alles: Repository-Hygiene, gofmt, go vet, staticcheck, govulncheck, die Tests mit dem
Race-Detektor und einen Browsertest in einem echten Chrome mit Handy-Bildschirm. Es braucht Go, Python 3, die
PostgreSQL-Server-Programme und Chrome. `scripts/check-image.sh` prüft das Container-Abbild. Siehe
[CONTRIBUTING](CONTRIBUTING.de.md).

## Lizenz

Der Code steht unter der [MIT-Lizenz](../LICENSE).

Das Logo steht **nicht** unter der MIT-Lizenz: Icon von [juicy_fish](https://www.flaticon.com/de/autoren/juicy-fish) bei
[Flaticon](https://www.flaticon.com/de/), genutzt unter der Flaticon-Lizenz mit Namensnennung, siehe
[assets/LICENSE-icon.md](../assets/LICENSE-icon.md).
