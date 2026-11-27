#!/usr/bin/env bash
# The gate before every push and merge: repository hygiene, the licenses shipped with the binary, then the Go part
# (gofmt, tidy, vet, staticcheck, govulncheck, tests with the race detector), the browser test and the residue check.
#
#   scripts/check.sh            everything
#   scripts/check.sh --fast     without govulncheck and the race detector (short loop while building)
#
# Repeatable: no state stays behind.
set -euo pipefail
cd "$(dirname "$0")/.."

FAST=0
[[ "${1:-}" == "--fast" ]] && FAST=1

step() { printf '\n\033[1m▸ %s\033[0m\n' "$1"; }
fail() { printf '\n\033[31m✗ %s\033[0m\n' "$1" >&2; exit 1; }

PY="$(command -v python3 || true)"
[[ -n "$PY" ]] || fail "python3 is missing (needed for the hygiene base)"
export PYTHONDONTWRITEBYTECODE=1

# Text fast path (kit): a text-only change runs just the hygiene including the text checks.
if grund="$(scripts/_nur_text.sh)"; then
    step "$grund"
    "$PY" tests/test_repo.py || fail "repository hygiene"
    exit 0
fi
step "$grund"

# Unused dependencies (kit 0.73.0, PO 2026-11-27): the app carries only what it uses. Allow list with reasons:
# .github/abhaengigkeiten.json. Fetches pinned tools (deptry, cargo-machete), so it runs here, not in the hook.
step "Unused dependencies (kit, scripts/_abhaengigkeiten.py)"
"$PY" scripts/_abhaengigkeiten.py || fail "unused dependencies (scripts/_abhaengigkeiten.py)"

step "Repository hygiene (kit)"
"$PY" tests/test_repo.py || fail "repository hygiene"

if command -v shellcheck >/dev/null; then
    step "shellcheck"
    shellcheck scripts/*.sh || fail "shellcheck"
fi

step "Third-party licenses in the binary"
scripts/third-party-licenses.sh --check || fail "third-party licenses"

# The browser test sits behind a tag, so it also has to pass vet and staticcheck explicitly.
export GO_PRUEF_TAGS="browser"
if [[ $FAST -eq 1 ]]; then
    scripts/_go_check.sh --fast
else
    scripts/_go_check.sh
fi

# A real Chrome on the screen of a phone. A missing Chrome fails: a skipped browser test is not a green one.
step "Browser test (phone, touch, passkeys)"
go test -count=1 -tags browser ./web/browsertest/ || fail "browser test"

step "Residue check"
rc=0
scripts/_residue_check.sh check || rc=$?
case "$rc" in
    0) : ;;
    2) printf '\033[33m! residue undecidable (working tree was already modified)\033[0m\n' ;;
    *) fail "residue check" ;;
esac

printf '\n\033[32m✓ all green\033[0m\n'
