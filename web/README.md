# The web interface

Server-rendered pages (`templates/`, Go `html/template`) in the design system [C22](https://github.com/Ollornog/C22), a
little JavaScript of its own (`assets/app/app.js`: dialogs, the question before removing, the theme, the passkey steps;
`sw.js`: the service worker), and nothing loaded from anywhere else. No package from npm is needed, so there is no
lockfile and no build step for JavaScript.

- `assets/c22/` is C22's build output, refreshed only by `scripts/vendor-c22.sh <commit>`; `VERSION` names the commit.
- `assets/licenses/THIRD-PARTY.txt` comes from `scripts/third-party-licenses.sh`; `scripts/check.sh` fails when it does
  not match the modules of the binary.
- `i18n/en.json` and `i18n/de.json` hold every text; a test keeps their keys and placeholders in step.
- `auth/` holds the accounts; `browsertest/` runs the program in a real Chrome (`go test -tags browser`).

## Everything from C22

The app has no stylesheet of its own: every class is one of C22's, and a test (`TestEveryClassExistsInTheStylesheet`)
fails on a class C22 lacks or on any CSS file in `assets/app/`. What the app needs and C22 lacks goes to C22 first.
Parts used beyond the plain components:

| Part | Where |
|---|---|
| Tab bar (`nav.tab-bar`, the open page `aria-current="page"`; the page keeps `pb-20` below its content) | `layout.html` |
| QR code (`img.qr-code`) and key to type off (`code.secret-key`) | setting up an authenticator app |
| Code block with wrapped lines (`pre.code-block[data-wrap]` with `max-h-96`) | the licenses page |
| Collapsible card (`details.card`, `<summary><h3>`, `<section>`) | absence, span |
| Blocks of the category "Data entry": day row, stat row, duration field | the week views |

A duration field carries `pattern` from `durationPattern` (Go, `track.go`), which a test keeps in step with
`parseHours`: the browser refuses early what the server would refuse, and nothing it would take.
