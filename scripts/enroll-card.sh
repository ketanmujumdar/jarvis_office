#!/usr/bin/env bash
# Enroll the office card with Reap (EXTERNAL source) through the running api.
#
#   scripts/enroll-card.sh [email]            # default: the seeded manager
#   API_BASE_URL=http://localhost:8080 scripts/enroll-card.sh
#
# It logs in (fake auth), starts an enrollment, prints the Reap-hosted card-entry URL (and opens it
# on macOS), then polls until the enrollment is ACTIVE. Card details are typed only into Reap's page;
# they never pass through this script, the api or the database. No API keys are needed here: the
# api holds REAP_API_KEY.
set -euo pipefail

API="${API_BASE_URL:-http://localhost:8080}"
EMAIL="${1:-maya.tan@example.com}"
TIMEOUT_S="${ENROLL_TIMEOUT_S:-600}"

json_get() { # json_get <python-expression on d> ; reads JSON on stdin
  python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"
}

curl -fsS "$API/healthz" >/dev/null || { echo "api not reachable at $API (run: make run)" >&2; exit 1; }

TOKEN="$(curl -fsS -X POST "$API/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\"}" | json_get "d['token']")"

current="$(curl -sS "$API/api/v1/enrollments/current" -H "Authorization: Bearer $TOKEN" || true)"
if [[ "$(printf '%s' "$current" | json_get "d.get('status','')" 2>/dev/null || true)" == "ACTIVE" ]]; then
  echo "A card is already enrolled and ACTIVE. Nothing to do."
  exit 0
fi

resp="$(curl -fsS -X POST "$API/api/v1/enrollments" -H "Authorization: Bearer $TOKEN")"
url="$(printf '%s' "$resp" | json_get "d.get('next_action_url','')")"
if [[ -z "$url" ]]; then
  echo "Enrollment created without a hosted URL: $resp" >&2
  exit 1
fi

echo "Open Reap's secure card-entry page and add the card:"
echo
echo "  $url"
echo
if command -v open >/dev/null 2>&1; then open "$url" || true; fi

echo -n "Waiting for the enrollment to become ACTIVE"
deadline=$(( $(date +%s) + TIMEOUT_S ))
while (( $(date +%s) < deadline )); do
  status="$(curl -fsS "$API/api/v1/enrollments/current" -H "Authorization: Bearer $TOKEN" | json_get "d.get('status','')")"
  case "$status" in
    ACTIVE) echo; echo "Card enrolled (ACTIVE). You can now confirm orders."; exit 0 ;;
    FAILED|EXPIRED|REVOKED) echo; echo "Enrollment ended with status $status. Run this script again." >&2; exit 1 ;;
  esac
  echo -n "."
  sleep 3
done
echo; echo "Timed out waiting for ACTIVE (status: $status)." >&2
exit 1
