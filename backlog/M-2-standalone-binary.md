---
id: M-2
type: Milestone
title: "The standalone program with a small web interface"
status: offen
blocked_by: [M-1]
tags: [ui, binary]
created: 2026-11-17
---

EightPerDay runs on its own (ADR-1): one binary, PostgreSQL, a small web interface for one person.

**Done when this is true:**

- `cmd/eightperday` serves the web interface from `web/`, with no resources from third parties.
- Every operating mode has its view, and the mode can be switched from the interface.
- The about dialog names the logo's author (juicy_fish, Flaticon), as its license requires.
- The release workflow builds and checks the binary.
