# Jarvis Office

A voice-driven procurement agent for office managers in Singapore. You say what the office needs, and Jarvis matches it to the approved catalog and compares prices at allow-listed merchants through Reap Agentic Payments. Plain code then enforces the spending policy and approvals. Payment happens on Reap's hosted approval page, so the app never handles card data.

- Product spec: [docs/PRD.md](docs/PRD.md). Where the two differ, [docs/MVP_DECISIONS.md](docs/MVP_DECISIONS.md) wins.
- Shared contracts (packages, status machine, SSE, conventions): [docs/CONTRACTS.md](docs/CONTRACTS.md)
- REST and SSE API: [docs/openapi.yaml](docs/openapi.yaml)
- Reap API snapshot, which is authoritative for Reap: [docs/reap/](docs/reap)

```
Flutter web app ──REST/SSE──▶ Go api (backend/cmd/api) ──▶ Postgres
                                  │  orchestrator · policy · approvals · in-process job runner
                                  ├──▶ OpenAI (parse, text agent, Realtime voice session)
                                  └──▶ Reap Agentic Payments (search → quote → checkout, card enrollment)
```

## Prerequisites

| Tool | Version |
| --- | --- |
| Go | 1.27 |
| Flutter | 3.47 (Chrome for web) |
| Docker | Docker Desktop or Engine, with Compose v2 |
| Python 3 | Used only by `scripts/enroll-card.sh` to read JSON |

## Quickstart

```bash
# 1. Secrets: copy the template and fill in your own keys. backend/.env is gitignored; never commit it.
cp backend/.env.example backend/.env
$EDITOR backend/.env          # OPENAI_API_KEY=…  REAP_API_KEY=… (sandbox key)

# 2. Postgres 17 in Docker. This also creates the jarvis_test database used by the tests.
make up

# 3. Schema and seed data: catalog, vendors, policy, 5 SG delivery addresses, demo users, system prompt.
make seed

# 4. The api on http://localhost:8080. It reads backend/.env.
make run
#    Without keys: `make run-fakes` serves the same API with fake Reap and LLM backends.

# 5. The Flutter web app on http://localhost:5173 (in a second terminal).
make app-run
#    which is the same as:
cd app && flutter run -d chrome --web-port 5173 --dart-define=API_BASE_URL=http://localhost:8080
```

Log in from the picker. Login is fake and there are no passwords.

| User | Role | Can |
| --- | --- | --- |
| Maya Tan | manager | Talk to Jarvis, confirm requests, pick the delivery address |
| Daniel Lim | approver | Approve or reject in the approver console |
| Priya Nair | admin | Everything, plus the admin pages: catalog, vendors, policy limits, addresses, system prompt |

## Enroll the office card (Reap-hosted page)

Requests cannot be confirmed until a card is enrolled. The API refuses with `409 no_active_enrollment`. The card is entered only on Reap's own page. Jarvis stores only the Reap enrollment id and its status. It never stores the card number, the expiry or the last four digits.

**From the app:** when no card is enrolled, the app's card action calls the same `POST /api/v1/enrollments` endpoint and opens Reap's card-entry page in a new tab. The status changes to `ACTIVE` once the card is added.

**From the terminal** (the api must be running):

```bash
make enroll                      # or: scripts/enroll-card.sh [manager-email]
```

The script does the following:

1. It logs in as the manager and calls `POST /api/v1/enrollments`. That call goes to Reap `POST /agentic/enrollments` with `source: EXTERNAL`.
2. It prints the hosted `next_action_url` and opens it on macOS.
3. It polls `GET /api/v1/enrollments/current` until the status is `ACTIVE`.

Notes for the Reap sandbox:

- Use the test card that Reap issued for your sandbox project. Agentic Payments accepts only cards that support Visa Token Service or the Cloud Token Framework (see `docs/reap/agentic-payments_setup.md`).
- After the card step, Reap redirects to `REAP_RETURN_URL`, which defaults to an HTTPS placeholder. You can close that tab. The api picks up the new status when it polls.
- At checkout, each merchant in the basket gets its own Reap approval page (`payments[].approval_url`). For sandbox demos without a real approval, `REAP_SIMULATE_CHECKOUT=true` sends `X-Simulate-Checkout: COMPLETED`. This works in the sandbox only.

## A typical run

1. Maya says or types: *"We're out of printer paper and coffee pods, reorder the usual."*
2. Jarvis parses the request into catalog items and uses the catalog's default quantities. It searches each preferred allow-listed merchant on Reap, ranks the offers and runs the policy. The progress streams to the UI over SSE.
3. Jarvis reads back the lines and the total, then asks **which delivery address** to use from the saved Singapore addresses.
4. On confirmation, auto-approved requests go straight to checkout. Off-list items or totals over a limit go to Daniel's approval queue, with the reasons and the best three quotes.
5. Checkout creates a live Reap quote for each merchant. If the live total has drifted more than `price_drift_pct` (5%) above the approved total, the request goes back for approval. Otherwise the payment approval link appears, and the order completes once the link is approved on Reap.

## Tests

| Command | What runs |
| --- | --- |
| `make test-go-unit` | Go unit tests. No database or network needed. |
| `make test-go` | Unit tests plus Postgres integration tests against the `jarvis_test` database |
| `make test-flutter` | `flutter analyze` and the widget tests |
| `make e2e` | The end-to-end suite (below) |
| `make smoke` | Real Reap sandbox and OpenAI calls (`-tags smoke`). Needs `backend/.env` and costs tokens. Never run it in CI. |

### End-to-end suite (`backend/e2e`, build tag `e2e`)

`make e2e` builds the real `cmd/api` binary and boots it against a fresh `jarvis_e2e` database on the compose Postgres. The api runs with its real HTTP clients. `REAP_BASE_URL` and `OPENAI_BASE_URL` point at wire-level fakes in `backend/e2e/harness`, which are `httptest` servers that speak the Reap agentic API and the OpenAI Chat Completions, Responses and Realtime client-secret APIs. The test then drives everything over HTTP and SSE, the way the app does:

- PRD example 1 (paper and coffee pods): parsing, catalog defaults, allow-list filtering, AUTO_APPROVE, address choice, one Reap quote and checkout per merchant, the hosted approval, and the final status `ordered`.
- PRD example 2 (a standing desk): an off-list item that needs approval, shown with the top 3 quotes. The test checks that only an approver can approve, and that a rejected request never reaches checkout.
- Card enrollment through the hosted page, and the `no_active_enrollment` guard.
- Delivery address selection, checked all the way into the Reap quote's `shippingAddress`.
- Price drift: a 4% drift goes through, and a 10% drift sends the request back to approval (`price_drift`) and emits the `checkout.price_drift` SSE event.
- The SSE event order, the per-request and global audit trail, the orders list and month-to-date spend.
- The voice tool relay (`/agent/tools/{name}`) and the text agent (`/agent/chat`).
- Editing the system prompt in admin, then checking that the new prompt appears in the next Realtime session config and in the text agent's model calls.
- Leak checks: no API key value or card digits in the logs, audit rows or responses.

Real keys are never used. The api runs with random throwaway keys, and `DOTENV` points at a missing file. Run a single step with `make e2e RUN='TestJourney/07'`.

## Repository layout

```
backend/            Go module (cmd/api, cmd/migrate, internal/*, seed/, e2e/)
app/                Flutter web app (lib/core design system, lib/features/*)
deploy/             docker-compose (Postgres 17; optional api container via `make up-all`)
scripts/            helper scripts (enroll-card.sh)
docs/               PRD, MVP decisions, contracts, OpenAPI, Reap docs snapshot
```

## Security notes

- `OPENAI_API_KEY` and `REAP_API_KEY` live only in `backend/.env`. The api logs a redacted config (`set` or `unset`). The browser gets only short-lived OpenAI Realtime client secrets.
- Card data never reaches this system. Card entry and payment approval happen on Reap-hosted pages.
- Authentication is a fake demo login: the token is the user id. Do not expose this deployment publicly.
