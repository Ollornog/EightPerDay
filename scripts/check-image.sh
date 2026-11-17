#!/usr/bin/env bash
# Builds the image and starts it hardened (read-only, no capabilities) next to the database image of compose.yaml: it
# has to get healthy, keep the link for the first account out of its log, make that link at the console (docker exec),
# and serve the sign-in and that link with its CSP. This is
# also the acceptance test for updates of the base images and of the database image.
#
#   scripts/check-image.sh        (docker; on the tower: sg docker -c scripts/check-image.sh)
set -euo pipefail
cd "$(dirname "$0")/.."

run="epd-check-$$"
image="$run:image"
net="$run-net"
db="$run-db"
app="$run-app"

fail() {
    printf '✗ %s\n' "$1" >&2
    docker logs "$app" 2>&1 | tail -20 >&2 || true
    exit 1
}
cleanup() {
    docker rm -f "$app" "$db" >/dev/null 2>&1 || true
    docker network rm "$net" >/dev/null 2>&1 || true
    docker rmi -f "$image" >/dev/null 2>&1 || true
}
trap cleanup EXIT

database_image="$(sed -n 's/^[[:space:]]*image:[[:space:]]*\(postgres:[^[:space:]]*\).*/\1/p' compose.yaml | head -1)"
[[ "$database_image" == postgres:*@sha256:* ]] || fail "compose.yaml names no pinned postgres image"
secret="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"

docker build --quiet -t "$image" . >/dev/null
docker network create "$net" >/dev/null
docker run -d --name "$db" --network "$net" -e POSTGRES_USER=eightperday -e POSTGRES_DB=eightperday \
    -e POSTGRES_PASSWORD="$secret" "$database_image" >/dev/null
docker run -d --name "$app" --network "$net" --read-only --cap-drop ALL --security-opt no-new-privileges \
    -e EIGHTPERDAY_DATABASE_URL="postgres://eightperday:$secret@$db/eightperday?sslmode=disable" \
    -e EIGHTPERDAY_ORIGIN=http://localhost:8080 -p 127.0.0.1::8080 "$image" >/dev/null

# The database starts first; the program waits for it, then its health check turns healthy. 120 tries of half a second.
for _ in $(seq 1 120); do
    [[ "$(docker inspect -f '{{.State.Health.Status}}' "$app")" == healthy ]] && break
    [[ "$(docker inspect -f '{{.State.Running}}' "$app")" == true ]] || fail "the container stopped"
    sleep 0.5
done
[[ "$(docker inspect -f '{{.State.Health.Status}}' "$app")" == healthy ]] || fail "the container does not get healthy"

port="$(docker port "$app" 8080/tcp | head -1 | sed 's/.*://')"
base="http://127.0.0.1:$port"
# The answers go into variables first: with pipefail, grep -q could end curl early and fail the pipe at random.
head="$(curl -fsSI "$base/login")" || fail "the sign-in does not answer"
grep -qi '^content-security-policy:' <<<"$head" || fail "the sign-in has no CSP"
logs="$(docker logs "$app" 2>&1)"
# Whoever reads the log must not find a link that makes them the administrator; the log only says how to make one.
[[ "$logs" != *'token='* ]] || fail "the log holds a one-time link"
[[ "$logs" == *'eightperday link --admin'* ]] || fail "the log does not say how to make the first account"
made="$(docker exec "$app" /eightperday link --admin)" || fail "the console makes no link for the first account"
pattern='http://localhost:8080/setup\?token=[A-Za-z0-9_-]+'
[[ "$made" =~ $pattern ]] || fail "the console printed no link for the first account"
link="${BASH_REMATCH[0]}"
setup="$(curl -fsS "$base${link#http://localhost:8080}")" || fail "the one-time link does not answer"
[[ "$setup" == *'data-setup'* ]] || fail "the one-time link does not show the setup"
user="$(docker inspect -f '{{.Config.User}}' "$image")"
[[ "$user" == 65532:65532 ]] || fail "the image runs as $user"
echo "✓ image: healthy, read-only, without capabilities, no link in the log, the first link from the console, the sign-in with its CSP"
