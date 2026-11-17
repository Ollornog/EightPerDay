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

## What C22 does not have yet

`assets/app/app.css` holds the few rules C22 lacks, from its tokens only. Each belongs in C22 eventually:

| Rule | Needed for |
|---|---|
| `.epd-tabbar` | a tab bar fixed to the bottom of a phone's screen, above the home indicator, hidden from 48rem on |
| `.epd-qr` | a QR code at a size a camera reads well, without smoothing |
| `.epd-secret`, `.epd-licenses` | long unbroken strings (a TOTP key, license texts) that wrap anywhere |
