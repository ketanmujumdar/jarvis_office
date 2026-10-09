#!/usr/bin/env bash
# One-command demo launcher for Jarvis Office.
#
#   scripts/demo.sh start    # (re)start everything and print the demo URL
#   scripts/demo.sh status   # show what is running
#   scripts/demo.sh redeploy # rebuild api + web after code changes, KEEP the tunnel URL
#   scripts/demo.sh stop     # stop web server + tunnel + containers
#
# start is safe to re-run at any time: it restarts whatever is down, opens a fresh
# Cloudflare quick tunnel, writes the new URL into backend/.env as REAP_RETURN_URL and
# recreates the api container so it picks the URL up.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STATE="$ROOT/.demo"
ENV_FILE="$ROOT/backend/.env"
COMPOSE=(docker compose -f "$ROOT/deploy/docker-compose.yml" --profile api)
WEB_PORT=5173
API_URL="http://localhost:8080"

mkdir -p "$STATE"
log() { printf '\033[1;36m▸ %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

kill_pidfile() {
  local f="$STATE/$1.pid"
  if [[ -f "$f" ]] && kill -0 "$(cat "$f")" 2>/dev/null; then kill "$(cat "$f")" 2>/dev/null || true; fi
  rm -f "$f"
}

wait_http() { # url, seconds
  for _ in $(seq 1 "$2"); do curl -sf -o /dev/null "$1" && return 0; sleep 1; done
  return 1
}

start() {
  [[ -f "$ENV_FILE" ]] || die "backend/.env missing (needs OPENAI_API_KEY and REAP_API_KEY)"
  docker info >/dev/null 2>&1 || die "Docker is not running. Start Docker Desktop and retry."
  command -v cloudflared >/dev/null || die "cloudflared not installed (brew install cloudflared)"

  log "Starting Postgres + api in Docker"
  "${COMPOSE[@]}" up -d --build --wait

  log "Applying migrations + seed (idempotent)"
  (cd "$ROOT/backend" && DATABASE_URL="postgres://jarvis:jarvis@localhost:5432/jarvis?sslmode=disable" go run ./cmd/migrate -seed) >/dev/null

  log "Building Flutter web (release)"
  # same-origin: the tunnel routes /api/* and /healthz to the api, so the browser never
  # calls http://localhost from an https page (Chrome blocks that, and phones can't reach it).
  (cd "$ROOT/app" && flutter build web --release --dart-define=API_BASE_URL=same-origin >/dev/null)

  log "Serving web app on :$WEB_PORT"
  kill_pidfile web
  lsof -ti tcp:$WEB_PORT | xargs kill 2>/dev/null || true
  nohup python3 -m http.server "$WEB_PORT" --bind 127.0.0.1 --directory "$ROOT/app/build/web" \
    >"$STATE/web.log" 2>&1 &
  echo $! >"$STATE/web.pid"
  wait_http "http://127.0.0.1:$WEB_PORT/" 20 || die "web server did not start (see .demo/web.log)"

  log "Opening Cloudflare quick tunnel"
  kill_pidfile tunnel
  # Own ingress (also overrides ~/.cloudflared/config.yml): api paths -> :8080, rest -> web.
  cat >"$STATE/cloudflared.yml" <<YAML
ingress:
  - path: ^/(api/|healthz)
    service: http://127.0.0.1:8080
  - service: http://127.0.0.1:$WEB_PORT
YAML
  : >"$STATE/tunnel.log"
  nohup cloudflared tunnel --config "$STATE/cloudflared.yml" --no-autoupdate \
    --url "http://127.0.0.1:$WEB_PORT" >"$STATE/tunnel.log" 2>&1 &
  echo $! >"$STATE/tunnel.pid"
  local url=""
  for _ in $(seq 1 40); do
    url=$(grep -o 'https://[a-z0-9-]*\.trycloudflare\.com' "$STATE/tunnel.log" | head -1 || true)
    [[ -n "$url" ]] && break; sleep 1
  done
  [[ -n "$url" ]] || die "tunnel did not report a URL (see .demo/tunnel.log)"
  echo "$url" >"$STATE/url"

  log "Setting REAP_RETURN_URL=$url/#/reap-return"
  grep -v '^REAP_RETURN_URL=' "$ENV_FILE" >"$ENV_FILE.tmp" || true
  echo "REAP_RETURN_URL=$url/#/reap-return" >>"$ENV_FILE.tmp"
  mv "$ENV_FILE.tmp" "$ENV_FILE"; chmod 600 "$ENV_FILE"

  log "Recreating api container with the new return URL"
  "${COMPOSE[@]}" up -d --force-recreate --wait api
  wait_http "$API_URL/healthz" 30 || die "api not healthy (docker logs jarvis-office-api-1)"

  log "Waiting for tunnel to go live (DNS can take ~30s)"
  local host="${url#https://}" ok=""
  for _ in $(seq 1 45); do
    # Resolve via 1.1.1.1 so a stale local negative DNS cache doesn't fool us.
    local ip; ip=$(dig +short @1.1.1.1 "$host" | head -1 || true)
    if [[ -n "$ip" ]] && curl -sf -o /dev/null --resolve "$host:443:$ip" "$url/"; then ok=1; break; fi
    sleep 2
  done
  [[ -n "$ok" ]] || log "Tunnel not reachable yet; give it a minute, then run: scripts/demo.sh status"

  printf '\n\033[1;32m✓ Jarvis is up\033[0m\n'
  printf '  Demo URL : \033[1m%s\033[0m   (use this one, not localhost)\n' "$url"
  printf '  API      : %s/healthz\n' "$API_URL"
  printf '  Logs     : docker logs -f jarvis-office-api-1 | .demo/tunnel.log | .demo/web.log\n'
  printf '  Note     : if the URL does not open on this Mac yet, wait ~1 min (local DNS cache).\n'
  command -v open >/dev/null && open "$url" || true
}

redeploy() {
  [[ -f "$STATE/url" ]] || die "no running demo; use: scripts/demo.sh start"
  log "Rebuilding + recreating api container"
  "${COMPOSE[@]}" up -d --build --force-recreate --wait api
  log "Rebuilding Flutter web (served in place)"
  (cd "$ROOT/app" && flutter build web --release --dart-define=API_BASE_URL=same-origin >/dev/null)
  wait_http "$API_URL/healthz" 30 || die "api not healthy (docker logs jarvis-office-api-1)"
  printf '\n\033[1;32m✓ Redeployed\033[0m  %s  (hard-refresh the page: Cmd+Shift+R)\n' "$(cat "$STATE/url")"
}

status() {
  "${COMPOSE[@]}" ps --format 'table {{.Name}}\t{{.Status}}' || true
  printf 'api health : %s\n' "$(curl -s "$API_URL/healthz" || echo DOWN)"
  for p in web tunnel; do
    if [[ -f "$STATE/$p.pid" ]] && kill -0 "$(cat "$STATE/$p.pid")" 2>/dev/null; then echo "$p       : running"; else echo "$p       : DOWN"; fi
  done
  [[ -f "$STATE/url" ]] && printf 'demo url   : %s (HTTP %s)\n' "$(cat "$STATE/url")" \
    "$(curl -s -o /dev/null -w '%{http_code}' "$(cat "$STATE/url")/" || true)"
}

stop() {
  kill_pidfile tunnel
  kill_pidfile web
  "${COMPOSE[@]}" down
  log "Stopped (database volume kept)"
}

case "${1:-start}" in
  start) start ;;
  status) status ;;
  redeploy) redeploy ;;
  stop) stop ;;
  *) die "usage: scripts/demo.sh [start|status|redeploy|stop]" ;;
esac
