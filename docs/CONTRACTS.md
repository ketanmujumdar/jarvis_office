# Jarvis Office: shared contracts

Written by the foundation agent on 2026-10-09. Read this with `docs/MVP_DECISIONS.md` (overrides the PRD), `docs/reap/*.md` (authoritative for Reap) and `docs/openapi.yaml` (authoritative for REST + SSE).

If you need a contract change, make the smallest additive change you can and log it under **Change log** at the bottom.

## 1. Layout and ownership

| Agent | Owns (edit only these) | Delivers |
| --- | --- | --- |
| A | `backend/internal/store/**` (except `migrate.go` semantics), `backend/internal/store/migrations/*.sql`, `backend/internal/store/seed/**`, `backend/cmd/migrate` | pgx implementation of every repo in `store/store.go` (replace `postgres/stubs.go`), new migrations as `0002_*.up.sql` and later, `seed.Load`, an in-memory `store/memstore` (implements `store.Store`) for other agents' unit tests, and integration tests against `TEST_DATABASE_URL` |
| B | `backend/internal/policy/**`, `backend/internal/approvals/**` | `policy.Evaluate` (pure, table-tested), `approvals.Impl` |
| C | `backend/internal/reap/**` | `reap.HTTPClient` (headers, idempotency, error decoding, retry on 503), `reap/fakereap` (an httptest server that mimics the endpoints with in-memory state), `VerifyWebhookSignature`, and `//go:build smoke` sandbox tests |
| D | `backend/internal/llm/**`, `backend/internal/realtime/**` | `llm.OpenAI`, `llm/fakellm` (scripted responses), `realtime.OpenAIMinter` and a fake, and smoke tests |
| E | `backend/internal/orchestrator/**`, `backend/internal/queue/**`, `backend/internal/agents/**` | the in-process runner, the orchestrator state machine, the Reap `VendorAdapter`, the parser, `RankOffers`, the `ToolExecutor` and the `TextAgent` |
| F | `backend/internal/api/**`, `backend/internal/audit/**`, `backend/internal/events/**`, `backend/cmd/api/**` | every handler in `api.Routes`, the SSE endpoint, fake auth, CORS, `audit.New`, and the `main.go` wiring (including `FAKES=true`) |
| G | `app/lib/features/voice/**`, `app/lib/features/request/**` | the voice/text playground (WebRTC, tool relay, transcript) and the request detail page (line items, quote table, approval state, checkout) |
| H | `app/lib/features/admin/**`, `app/lib/features/auth/**` | the login picker and the admin pages for catalog, vendors, policy, addresses and the system prompt |
| I | `app/lib/features/approvals/**`, `app/lib/features/orders/**` | the approver console, order history, the monthly spend chart (fl_chart) and the audit trail |
| J | `backend/e2e/**`, `Makefile`, `deploy/**`, `backend/Dockerfile` | end-to-end tests (`-tags e2e`) and polish of Makefile and compose |

Shared but frozen (foundation): `backend/internal/domain/**`, `backend/internal/config/**`, `backend/internal/store/store.go`, `backend/internal/store/migrate.go`, `app/lib/core/**`, `docs/openapi.yaml`. Change these only additively, and log the change.

Flutter feature owners register sub-routes in `app/lib/core/router.dart` inside their own branch. That is the one allowed edit to core.

## 2. Backend packages

Module: `github.com/ketanmujumdar/jarvis_office/backend` (Go 1.27). There is one binary, `cmd/api`, plus `cmd/migrate`.

| Package | Key contract |
| --- | --- |
| `domain` | All shared types (`models.go`), `RequestStatus` and its state machine (`status.go`: `CanTransition`, `CheckTransition`), `Cents` money (`money.go`) and sentinel errors (`errors.go`). It has no internal imports. |
| `config` | `config.Load(dotenvPath)` loads env vars, with `backend/.env` filling in only keys that are unset. `Config.Redacted()` is the only form you may log. |
| `store` | Repository interfaces (`store.Store` aggregate, `WithTx`), and `store.Migrate(ctx, pool)` (embedded SQL, advisory lock, idempotent). |
| `store/postgres` | `postgres.Open(ctx, dsn)` and `postgres.New(pool)`. Repo methods are stubs until agent A replaces them. |
| `store/seed` | `seed.Parse(dir)` (implemented and validated) and `seed.Load(ctx, store, dir, opts)` (stub). |
| `policy` | `Evaluate(Input) Result` (stub; it fails closed to NEEDS_APPROVAL) and `DriftExceeded(approved, live, pct)` (implemented). |
| `approvals` | `Service` interface, `DecisionHandler` callback (the orchestrator implements it), `Impl.SetHandler` to break the cycle. |
| `reap` | `Client` interface: SearchProducts, GetProductDetails, ResolveVariant, CreateQuote, GetQuote, SelectShippingOption, CreateCheckout, GetCheckout, CreateEnrollment, GetEnrollment. Request and response structs mirror `docs/reap` field for field. `*APIError` carries Reap error codes. There are no mandates. |
| `llm` | `Client.Chat(ChatRequest) ChatResponse`, provider-neutral tool calling with optional JSON-schema output. |
| `realtime` | `Minter.Mint(SessionParams) Session`, which returns an ephemeral OpenAI Realtime client secret and the calls URL. |
| `queue` | `Runner` (Register, Enqueue, EnqueueAfter, Start, Stop), the `Job` envelope `{v, job_id, request_id, line_item_id, kind, payload, deadline, attempt}` and the job kinds. |
| `agents` | Tool names and argument structs, `ToolExecutor`, `TextAgent`, `Parser`, `VendorAdapter`, `SearchQuery`. |
| `orchestrator` | `Service` (CreateRequest, GetRequest, ListRequests, Confirm, Cancel, RefreshPayments, StartEnrollment, CurrentEnrollment, OnApprovalDecided, Resume), `Deps`, and `RankOffers`. |
| `events` | `Bus` (Publish, Subscribe) plus the implemented `events.NewMemory`. The SSE event type constants are listed in section 5. |
| `audit` | `Logger.Record(ctx, requestID, actorType, actorID, type, payload)` and the audit type constants. |
| `api` | `NewServer(Deps).Handler()`, the canonical `Routes` table (a test keeps it in sync with openapi.yaml), and `StatusFor(err)` for error mapping. |

Dependency direction: `api → orchestrator, approvals, agents, realtime → reap, llm, policy, store, queue, events, audit → domain`. Nothing imports `api`. `approvals` must not import `orchestrator`; use `DecisionHandler` instead.

### Interfaces with fakes (all tests run offline)

| Dependency | Interface | Fake (to build) |
| --- | --- | --- |
| Postgres | `store.Store` | `store/memstore` (A). Integration tests use real Postgres. |
| Reap | `reap.Client` | `reap/fakereap` httptest server (C), used through the real `HTTPClient` |
| OpenAI chat | `llm.Client` | `llm/fakellm` (D) |
| OpenAI Realtime | `realtime.Minter` | fake minter (D) |
| Jobs | `queue.Runner` | the in-process runner itself, which is deterministic with `Workers: 1` |
| Clock | `func() time.Time` in `orchestrator.Deps.Clock` | fixed clock |

## 3. Reap facts from the sandbox probe (2026-10-09)

- Base URL `https://sg.sandbox.api.reap.global`. Every call sends `Authorization: Bearer $REAP_API_KEY` and `Reap-Version: 2025-02-14`. POST to quotes, checkouts and enrollments also sends an `Idempotency-Key`, which must be stable per logical attempt: store it in `payments.idempotency_key`.
- **`merchantPreference.merchantName` must be the merchant domain** (for example `popular.com.sg`). Every allowed domain resolved. Display names often fail with `400 MERCHANT_NOT_RESOLVED`: `"Popular Bookstore"`, `"UGREEN SG"` and `"ZENXIN ORGANIC"` all failed.
- Search results carry only `merchant.name`, which differs from the domain. `vendors.reap_merchant_name` stores the exact string, for example `"Metro Singapore Departmental Store - Celebrating 69 Years in SG"`. Keep only products whose `merchant.name` equals the vendor's `reap_merchant_name` and whose vendor is `allowed`.
- **Anker (`anker.com.sg`) returns `merchant.name == "---"`.** Trust that name only when the search used `merchantPreference {ONLY, anker.com.sg}`.
- `greatjonesgoods.com` returned nothing, so it is seeded with `allowed=false` and an empty merchant name.
- Searches without a merchant preference return many non-allowlisted merchants (ShopMustafa, Waangoo, Stationery Pal and others). Always search per vendor with `mode: ONLY` and filter.
- The sandbox intermittently returns `503 AGENTIC_SERVICE_UNAVAILABLE` under parallel load (about 6 concurrent requests). Retry with backoff, and keep per-request fan-out concurrency at 4 or lower.
- Search returns `previewVariant` (id and price). Details return `defaultVariant` and `options`. Products with options need `POST /agentic/products/variant` to choose non-default options, but the default variant is purchasable as is. `requiresShipping` is often absent: always send the shipping address.
- Prices are decimal numbers in SGD (`35.5`). Convert with `domain.CentsFromFloat`.
- A quote takes `items` (all variants from one merchant), `email` (use the address email) and `shippingAddress` (mapped from `domain.Address`). One `payments` row is created per merchant in the basket.
- A checkout takes `quoteId`, `enrollmentId` (Reap's id) and `presentation {REDIRECT, returnUrl (HTTPS)}`. The response has `nextAction.url`, the Reap-hosted approval page. Store it as `payments.approval_url` and poll `GET /agentic/checkouts/:id` until it reaches COMPLETED, FAILED or EXPIRED. `orderId` and `finalAmount` arrive on COMPLETED. In the sandbox, `REAP_SIMULATE_CHECKOUT=true` sends `X-Simulate-Checkout: COMPLETED`.
- Enrollment is `POST /agentic/enrollments {source: EXTERNAL, owner {CLIENT_REFERENCE, id: user id, email}, presentation}`. The user opens `nextAction.url` to enter the card on Reap's page. The enrollment must be `ACTIVE` before checkout. **Never store card data.** `GET` returns `paymentMethod.last4`, and we deliberately do not persist it.
- Mandates are not live. Do not call `/agentic/mandates`.
- Webhooks are optional (`REAP_WEBHOOKS_ENABLED`). The signature is `X-Reap-Webhook-Signature: t=..,v1=hex(hmac_sha256(secret, "t.body"))` with a 5-minute tolerance. Polling is the default.

## 4. PurchaseRequest status machine

```
parsing → searching → quoted ──confirm(address_id)──┬─ all AUTO_APPROVE ─→ approved
                                                    └─ NEEDS_APPROVAL ──→ pending_approval ─approve→ approved
                                                                                      └─reject─→ rejected
approved → checking_out ─┬─ live quote drift > price_drift_pct → pending_approval (approval kind price_drift)
                         └─ ok → awaiting_payment (payments.approval_url shown) → paying → ordered
any non-terminal → failed (failure_reason) | cancelled (not from paying)
quoted → rejected (every line REJECT) ; quoted → searching (re-search allowed)
```

`domain.CanTransition` is the single source of truth, and `RequestRepo.TransitionStatus` enforces it atomically (compare-and-set on `from`). The orchestrator flow is:

1. **CreateRequest.** Insert the request with status `parsing`, then publish `request.created`.
2. **Parse.** The LLM produces items, and the deterministic catalog match runs: exact SKU first, then name or alias (case-insensitive, fuzzy), then off-list. Missing qty becomes `default_qty`. Then `line_items.parsed` and the move to `searching`.
3. **Search.** One job per line item × preferred vendor (off-list items go to every office-relevant allowed vendor), with the 20 s `SEARCH_DEADLINE`. Each job publishes `search.vendor_result`. Rank whatever arrives by the deadline.
4. **Rank and policy.** Select the best offer per line, run `policy.Evaluate` with month-to-date spend, store decisions and reasons, then publish `offers.ranked` and `policy.evaluated` and move to `quoted`.
5. **Confirm(address_id).** Requires an ACTIVE enrollment, otherwise `409 no_active_enrollment`. Store the address, then go to `approved` or create a policy approval.
6. **Checkout.** Per merchant: CreateQuote, keep the preselected shipping option, take `amountBreakdown.finalAmount` as the live total, compare it with the approved total via `policy.DriftExceeded`, then call CreateCheckout. Publish `payment.action_required` with the URL and enqueue a poll.
7. **Poll.** COMPLETED records `reap_order_id` and `final_cents`. When every payment is completed, publish `order.completed` and move to `ordered`. FAILED or EXPIRED moves to `failed`.

**LLM proposes, code decides.** Only the parse step and the text agent call the LLM. Policy, approvals, drift checks and checkout are plain code.

## 5. SSE (`GET /api/v1/events?request_id=&token=`)

Each frame has the form `id: <n>\nevent: <type>\ndata: <json of events.Event>\n\n`, where `events.Event` is `{id, type, request_id, at, data}`. A heartbeat is sent every 15 s. The types and their data shapes:

| type | data |
| --- | --- |
| `request.created` | `{request}` |
| `request.status_changed` | `{request_id, from, to, failure_reason?}` |
| `line_items.parsed` | `{request_id, line_items}` |
| `search.started` | `{request_id, line_item_id, vendors: [domain]}` |
| `search.vendor_result` | `{request_id, line_item_id, vendor_domain, offers_found, error?}` |
| `offers.ranked` | `{request_id, line_item_id, offers (top 3)}` |
| `policy.evaluated` | `{request_id, decision, reasons, lines, total_cents}` |
| `approval.requested`, `approval.decided` | `{approval}` |
| `checkout.quoted` | `{request_id, payment}` |
| `checkout.price_drift` | `{request_id, approved_cents, live_cents, pct}` |
| `payment.action_required` | `{request_id, payment, approval_url}` |
| `payment.status_changed` | `{request_id, payment}` |
| `order.completed` | `{request_id, payments}` |
| `enrollment.updated` | `{enrollment}` |
| `agent.message` | `{session_id, role, content}` |
| `heartbeat` | `{}` |

Delivery is best-effort. Clients re-fetch `GET /requests/{id}` on reconnect. Durable history lives in `audit_events`.

## 6. Conventions

- **Money** is `int64` cents of SGD everywhere: Go `domain.Cents`, DB `BIGINT *_cents`, JSON `*_cents`, Dart `int`. The only floats are Reap's wire amounts (convert at the boundary) and seed YAML `*_sgd` values (converted by the loader). Format with `Cents.SGD()` in Go and `Fmt.money()` in Dart.
- **IDs** are UUID strings (DB `gen_random_uuid()`). Reap ids are kept verbatim in `reap_*` columns.
- **Errors**: wrap the sentinels in `domain/errors.go`. HTTP maps them via `api.StatusFor` to `{"error":{"code","message"}}`, with codes `validation_failed` 400, `unauthorized` 401, `forbidden` 403, `not_found` 404, `conflict` / `invalid_transition` / `no_active_enrollment` 409, `upstream_error` 502 and `not_implemented` 501.
- **Auth** is fake. `POST /api/v1/auth/login {email}` returns `{token, user}`, and the token is the user id. Clients send `Authorization: Bearer <token>`, or `?token=` for SSE. The roles are `manager`, `approver` and `admin`, with demo users in `seed/users.yaml`.
- **Secrets**: `OPENAI_API_KEY` and `REAP_API_KEY` live only in `backend/.env`, which is gitignored. Never log or print them, and never put them in tests, docs or fixtures. Log `cfg.Redacted()` only.
- **Card data** is never stored: no PAN, expiry or last4. We keep only Reap enrollment ids and statuses.
- **Audit**: every state change appends an `audit_events` row. A DB trigger enforces append-only.
- **Time**: UTC in the DB. Budget months use Asia/Singapore (`store.SingaporeLocation`).
- **System prompt**: `system_prompts` key `agent`, seeded from `seed/system_prompt.md` only if missing. Both the text agent and the realtime session load it on every new session.
- **Models**: `OPENAI_MODEL` defaults to `gpt-5.1` and `OPENAI_REALTIME_MODEL` to `gpt-realtime`. Both were verified as available to the key on 2026-10-09.
- **Flutter**: the design system is in `app/lib/core/theme` (`AppSpace`, `AppRadius`, `JarvisColors` via `context.jc`, Inter type). Shared widgets in `app/lib/core/widgets` are `AppShell`, `AppCard`, `StatTile`, `StatusChip.*`, `EmptyState`, `PageHeader`, `PageScaffold`, `MoneyText` and `AsyncValueView`. Never hard-code colours or spacing in features. State uses Riverpod 3 (`apiProvider`, `sessionProvider`, `currentUserProvider`, `eventStreamProvider(requestId)`). Override `apiProvider` in widget tests and set `AppTheme.useGoogleFonts = false`.

## 7. Seed data (`backend/seed`)

| File | Content |
| --- | --- |
| `allowed_merchants.tsv` | The input allow-list of 50 domains |
| `vendors.yaml` | 50 vendors (49 allowed, 28 office-relevant) with probe notes |
| `catalog.yaml` | 35 items across Paper & Stationery, Coffee & Tea, Pantry & Snacks, Cleaning & Hygiene, and Electronics & Office. Every preferred vendor stocked the item in the probe. Electronics, chairs and monitors are `auto_approve: false`. |
| `policy.yaml` | S$500 per order, S$3,000 a month, 5% drift |
| `addresses.yaml` | 5 Singapore office addresses (one default) |
| `users.yaml` | Maya (manager), Daniel (approver), Priya (admin) |
| `system_prompt.md` | The default agent prompt: read back line items, ask which address, never claim payment until the status is `ordered` |

`seed.Parse` validates referential integrity, and `go test ./internal/store/seed` guards the committed files.

## 8. Running and testing

```bash
make up            # Postgres 17 via docker compose (also creates jarvis_test DB)
make migrate       # apply migrations to dev DB
make seed          # migrations + seed (once seed.Load is implemented)
make run           # api on :8080 (reads backend/.env)
make run-fakes     # api with FAKES=true (no external calls)
make app-run       # Flutter web on :5173 → API_BASE_URL=http://localhost:8080
make test          # test-go (unit + Postgres integration) + test-flutter (analyze + widget tests)
make test-go-unit  # Go unit tests only, no DB needed
make e2e           # backend/e2e with -tags e2e against jarvis_test DB (agent J)
make smoke         # -tags smoke real Reap sandbox + OpenAI (needs backend/.env; never in CI)
```

- Go integration tests read `TEST_DATABASE_URL`, which defaults in the Makefile to `postgres://jarvis:jarvis@localhost:5432/jarvis_test?sslmode=disable`. They skip when it is unset. They may drop and recreate the schema of that database only. testcontainers-go is also acceptable.
- Use table-driven tests. Reap is tested through `reap/fakereap` (an httptest server), and the LLM through `llm/fakellm`.
- Smoke tests use `//go:build smoke`, load keys via `config.Load("../../.env")`, and must not print secrets.
- Flutter uses widget tests under `app/test/`, with `test/helpers.dart` providing `themed()` and `setSurface()`.

## Change log

- 2026-10-09 foundation: initial contracts.
- 2026-10-09 H (flutter-admin): added `GET /api/v1/admin/system-prompt/default` -> `{key, content}` (the seeded `seed/system_prompt.md` text, read-only) to `docs/openapi.yaml`, to the `api.Routes` table (currently `notImplemented`; **owner F: implement the handler**, e.g. read the seed file or keep the seeded text in the system prompt repo), and `JarvisApi.defaultSystemPrompt()` in `app/lib/core/api/api_client.dart`. The admin "Reset to default" button loads this text into the editor; the admin then saves it with the existing `PUT`. The UI shows a friendly message while the endpoint returns 501.
- 2026-10-09 H (flutter-admin): admin sub-routes `/admin/{catalog|vendors|policy|addresses|prompt|card}` registered in the admin branch of `app/lib/core/router.dart`; `/admin` shows the catalog tab. The existing `/admin` prefix guard covers them.
- 2026-10-09 agent B (policy/approvals), additive only:
  - `policy.LineInput.Description` (optional label for reason messages) and `policy.LineResult.TotalCents` (JSON `total_cents`, the line amount counted toward the order; 0 for REJECT).
  - New helpers `policy.DriftPct(approved, live)` and `policy.CheckDrift(approved, live, cfg) (ok, reason)`, which returns a ready `PRICE_DRIFT` reason for the price_drift approval. `DriftExceeded` treats a NaN or negative pct as 0 (fail closed).
  - Policy fails closed: qty < 1 → REJECT/`NO_OFFER`; a negative price → REJECT/`NO_OFFER`; an empty offer currency → REJECT/`CURRENCY_MISMATCH`; an offer whose `vendor_id` differs from the vendor passed in → REJECT/`VENDOR_NOT_ALLOWED`; an inactive catalog item → `OFF_LIST`; no lines → REJECT. The line total is max(landed, unit×qty+shipping), and the sums saturate. No new reason codes were added (the openapi enum is unchanged).
  - `approvals.Deps.Events` (optional `events.Bus`) and `Impl.SetEvents`. `Create` also publishes `request.status_changed` and writes audit `request.status_changed` (inside the same tx). Audit rows are appended through `tx.Audit()` and not through `audit.Logger`, so they commit atomically with the state change.
  - `Approve`/`Reject` also return `ErrConflict` when the request is no longer `pending_approval` (for example, cancelled). If the `DecisionHandler` fails after the commit, the decided approval is returned **with** a wrapped error. The decision stays recorded.
  - `approvals.TopOffers(offers, n)` is exported: available offers first, then by rank (rank ≤ 0 last), stable.
- 2026-10-09 agent D (llm, realtime), additive only:
  - `llm.OpenAI` now uses Chat Completions (`POST {base}/chat/completions`), with function tools, strict `json_schema` output, and retries on 429/5xx. `OpenAIOptions` gained `MaxRetries` and `RetryBackoff`. Errors are `*llm.APIError`, which wraps `domain.ErrUpstream`, and key-like strings are redacted from them. Verified live with `gpt-5.1`.
  - New helpers in `llm` for agent E's parser and ranking:
    - `ExtractLineItems(ctx, client, ExtractInput{Utterance, Catalog: CatalogHints(items)})` returns `[]ExtractedItem{Description, Qty *int, Urgency, CatalogSKU}`. Its JSON matches `agents.ParsedItem`.
    - `MatchOffers(ctx, client, TargetFromCatalog(item), []OfferCandidate)` returns `[]OfferMatch{IsMatch, PackSize, UnitPriceCents, WithinMaxUnitPrice}`. The LLM judges the match and pack size. Code computes the unit price and the ceiling check, and prices are never sent to the model.
    - Deterministic helpers: `GuessPackSize`, `UnitPrice`, `NormalizeText`.
  - `llm/fakellm.New()` is a rule-based offline client that handles extraction, matching and the agent tool loop (create, status, list_addresses, then confirm_order with address_id, and cancel). Use `Push`, `PushText`, `PushToolCall` or `PushError` to script responses, and `Requests()` to inspect what was sent. Use it for `FAKES=true`.
  - `realtime.OpenAIMinter` calls `POST {base}/realtime/client_secrets` with `{expires_after, session: {type: realtime, model, instructions, audio: {input.transcription, output.voice}, tools (flat function tools), tool_choice: auto}}` and returns `calls_url = {base}/realtime/calls`. Verified live with `gpt-realtime`.
  - `realtime.Session` gained `Tools` (openapi `RealtimeSession.tools`).
  - New in `realtime`:
    - `realtime.Service`: `NewService(ServiceOptions{Minter, Prompts: store.SystemPrompts(), Tools: executor.Definitions, Voice})` and `.Start(ctx, userID)`. It loads the DB prompt with key `agent` on every session and appends a short voice addendum. It falls back to `FallbackInstructions` when the prompt is not found, and to `realtime.ToolDefinitions()` when no tools are supplied.
    - `realtime.ToolDefinitions()`: the canonical JSON schemas for create_order_request, get_request_status, list_addresses, confirm_order (requires request_id and address_id) and cancel_request. Agent E may return these from `ToolExecutor.Definitions()`.
    - `realtime.NewFake()`: an offline Minter.
  - Wiring for agent F in cmd/api: pass `realtime.NewFake()` and `fakellm.New()` when `cfg.UseFakes`. Have the `/realtime/session` handler call `Service.Start`.
- 2026-10-09 agent C (reap). Every change is additive:
  - `reap.Options` gained `MaxRetries` (0 means the default of 3, a negative value turns retries off) and `RetryBackoff` (default 250ms, doubled on each attempt, with `Retry-After` honoured up to 10s). Transport errors, 429 and 5xx are retried. Each retry sends the same body and the same `Idempotency-Key`. Other 4xx responses are never retried. `Timeout` applies to each attempt (default 30s).
  - New in `reap`:
    - Helpers: `NewIdempotencyKey()` (UUIDv4), `DefaultBaseURL`, `DefaultVersion`, `CodeAddressLine2Required`, `(*APIError).Reason()` (`detail.reason`, such as INVALID_PHONE or CARD_NOT_CAPTURED), `IsReason(err, reason)`, and `*TransportError` (unwraps to `domain.ErrUpstream`).
    - Webhooks: `ParseWebhook(header, rawBody, secret, now)`, `SignWebhook(secret, t, rawBody)` for tests, and the errors `ErrWebhookMalformedHeader`, `ErrWebhookStale` and `ErrWebhookBadSignature` (all wrap `domain.ErrUnauthorized`) and `ErrWebhookNoSecret`.
  - The client validates some input before sending anything and returns `domain.ErrValidation` without a request: an empty search query, 0 or more than 10 product ids, an empty or overlong idempotency key, and empty ids. The client fills in defaults when fields are empty: enrollment `source` EXTERNAL, `owner.type` CLIENT_REFERENCE and `presentation.type` REDIRECT.
  - `reap/fakereap` (the alias `reap/reaptest` re-exports it):
    - Setup: `fakereap.New(fakereap.Options{...})` returns a `*Server` with `.URL`, `.Close()` and `.Client(mutators...)`. `.Client` returns a real `*reap.HTTPClient` with fast retries. Options are `Clock`, `QuoteTTL` (15m), `ActionTTL` (30m), `Products`, `AutoActivateEnrollments`, `AutoApproveCheckouts` and `AllowHTTPReturnURL`. Use the last one only for local demos, because real Reap requires HTTPS.
    - Fixtures: `fakereap.DefaultProducts()` returns SG products from allowed merchants, plus 3 products from merchants that are not on the allow-list. Variant ids are stable, for example `var_pop_ik_copier_a4_80g`. Merchant names match `vendors.yaml`, and Anker's is `---`. `merchantPreference.merchantName` must be the domain.
    - Quotes: Standard delivery is S$4, free from S$60, and preselected. Express is S$12. The fake adds 9% GST included in the price. Offer code `SAVE10` takes 10% off, and `EXPIRED10` returns OFFER_CODE_EXPIRED.
    - Test hooks:
      - Enrollments: `ActivateEnrollment`, `FailEnrollment`, `RevokeEnrollment`.
      - Checkouts: `ApproveCheckout` moves the checkout to PROCESSING, and the next GET returns COMPLETED with an `orderId`. Also `CompleteCheckout`, `FailCheckout`, `ExpireCheckout`, `LatestCheckoutID`, `CheckoutIDs`.
      - Quotes and prices: `ExpireQuote`, `SetVariantPrice` (to test drift), `SetVariantAvailable`.
      - Server: `Advance(d)` moves the clock, `InjectFault(Fault{Method, Path (a trailing * acts as a wildcard), Status, Code, Times, RetryAfter})` injects errors, and `Requests()`, `RequestsTo(method, path)` and `Counts()` report traffic.
    - Hosted pages need no auth. `GET {URL}/hosted/enrollments/{id}` and `GET {URL}/hosted/checkouts/{id}` apply the action and return a 303 redirect to `returnUrl`. Add `?decision=decline` to fail the action instead. This makes the FAKES=true demo clickable.
    - `X-Simulate-Checkout: COMPLETED` (or `AutoApproveCheckouts`) makes the first GET of a checkout return COMPLETED. The create response still includes `nextAction`, as the schema requires.
  - Smoke test: `go test -tags smoke -run Smoke ./internal/reap/`. It runs read-only search and details calls against the sandbox, and it passed on 2026-10-09.
- 2026-10-09 agent A (store): `store/postgres` repos implemented (stubs removed). Additive changes:
  - Migration `0002_payments_completed_at`: `payments.completed_at` (set by `PaymentRepo.Create/Update` the first time status is `completed`). `SpendRepo` buckets by `completed_at` in Asia/Singapore and sums `COALESCE(final_cents, quoted_cents)`; `MonthSpend.Orders` counts distinct requests.
  - `seed.Options.Overwrite` and `seed.Apply(ctx, store, data, opts)`. Default `Load` (AUTO_SEED) upserts users by email, but seeds vendors, catalog and addresses only when that table is empty, the policy only when missing and the prompt only when missing, so admin edits and deletions survive restarts. `Overwrite` upserts vendors/catalog/addresses/policy by natural key (never the prompt, never deletes). `cmd/migrate -seed -overwrite` exposes it.
  - `store/memstore`: `memstore.New()` / `NewWithClock(clock)` implement `store.Store` in memory with the same FK, unique, ordering and state-machine semantics (`SetPaymentCompletedAt` is a test helper). Inside `WithTx` use only the tx-bound store.
  - `store/storetest.Run(t, factory)`: conformance suite run by both memstore (offline) and postgres (integration).
  - `store/pgtest.NewDatabase(t)`: integration tests get a throw-away database `jarvis_t_<rand>` created on the `TEST_DATABASE_URL` server and dropped afterwards (skips when unset), so packages can run in parallel without `DROP SCHEMA` races. Use it instead of resetting `jarvis_test`'s schema.
  - Malformed UUIDs passed to Get/Update/Delete return `ErrNotFound`; check, FK and format violations return `ErrValidation`; FK violations on delete return `ErrConflict`.
- 2026-10-09 J (e2e): `make e2e` boots the real `cmd/api` binary against its own database `jarvis_e2e` (dropped and recreated next to `TEST_DATABASE_URL`; `E2E_DATABASE_URL` overrides) with `FAKES=false` and `REAP_BASE_URL` / `OPENAI_BASE_URL` pointing at wire-level fakes in `backend/e2e/harness`. So the real `reap.HTTPClient`, `llm.OpenAI` (Chat Completions) and `realtime.OpenAIMinter` (`/realtime/client_secrets`) are exercised. Added `make enroll` (`scripts/enroll-card.sh`, hosted card-entry page) and `make e2e RUN=<pattern>`.
- 2026-10-09 F (api/audit/cmd/api), additive only:
  - `POST /api/v1/auth/login` also accepts `{name, role}`: an unknown or omitted email plus a name and a role upserts a demo user (email `<name-slug>.<role>@demo.jarvis.local`). Login sets the `jarvis_token` cookie (HttpOnly, value = token). Auth reads the token from `Authorization: Bearer`, then the cookie, then `?token=`. `docs/openapi.yaml` was updated: `email` is no longer required, and `name` and `role` were added.
  - Role checks: `POST /requests`, `/requests/{id}/confirm` and `/requests/{id}/cancel` need `manager|admin`. `GET /approvals` and approve/reject need `approver|admin`. `/admin/*` needs `admin`. Every other route needs any logged-in user. A path `{id}` that is not a UUID returns 404, except agent session ids.
  - `GET /api/v1/admin/system-prompt/default` is implemented. It reads `<SEED_DIR>/system_prompt.md` and falls back to `realtime.FallbackInstructions`.
  - `POST /realtime/session` goes through `realtime.Service` (agent D): the DB prompt plus the voice addendum, and the tools from `agents.ToolExecutor.Definitions`.
  - Approve/reject: if the approvals service returns a decided approval together with a non-sentinel error (the `DecisionHandler` failed after the commit), the API answers 200 with the approval and logs the error.
  - `api.Deps` gained optional `Logger`, `Clock`, `HeartbeatInterval` (SSE, default 15s) and `VerifyWebhook` (default `reap.VerifyWebhookSignature`).
  - Webhook receiver (`REAP_WEBHOOKS_ENABLED`): it verifies the signature and dedupes on the envelope `id`. It records audit `reap.webhook_received`, then refreshes from Reap in the background (by checkout id → `RefreshPayments`, or by enrollment id → `CurrentEnrollment`). The payload is never trusted for state.
  - `audit`: new types `reap.webhook_received` and `user.logged_in`; `audit.Build`, `audit.Scrub`/`IsSensitiveKey` (card/secret keys are redacted before writing, as a safety net), `audit.NewMemory()` and `audit.Nop{}` for other packages' tests.
  - `cmd/api` with `FAKES=true` starts an in-process `fakereap` server, so opening a hosted enrollment or checkout URL completes it, and uses `fakellm` and `realtime.NewFake()`. With `REAP_SIMULATE_CHECKOUT=true`, checkouts auto-approve in fakes mode. `cmd/api/main_test.go` drives the whole demo flow (login → enroll → request → quoted → confirm → approve → pay → ordered → chat/realtime) over memstore and fakes.
- 2026-10-09 agent E (queue, agents, orchestrator), additive only:
  - `queue`: `NewInProcess` now returns the concrete `*queue.InProcess` (it still satisfies `queue.Runner`). Added `Options.Logger`, `queue.Permanent(err)` / `IsPermanent` (a handler error that must not be retried), `ErrStopped` / `ErrQueueFull` / `ErrUnknownKind`, `NewID()` (UUIDv4), and on `*InProcess`: `WaitIdle(ctx)` (waits for queued, running and retrying jobs, but **not** for `EnqueueAfter` jobs whose delay has not elapsed, so a self-rescheduling poller does not block it), `Scheduled()` and `Status(jobID)`. Idempotency: enqueueing a `JobID` that is pending, running or completed is a no-op that returns the same id; a job that failed for good may be enqueued again. `Job.Deadline` becomes the handler context deadline; expired jobs are not retried.
  - `agents.SearchQuery` gained `Open bool` + `Allowed []domain.Vendor` (one search without merchantPreference, filtered to allow-listed `reap_merchant_name`s; Anker's `---` is only trusted under ONLY) and `CatalogUnit string` (for pack-size normalisation).
  - `domain.Offer.PackSize` semantics: the number of **catalog units** one purchasable variant contains (>= 1; conservative, see `agents.UnitsPerVariant`). The orchestrator buys `agents.PurchaseQty(qty, PackSize)` variants, `LandedCostCents = UnitPriceCents * that + ShippingCents`, and passes that variant count as `policy.LineInput.Qty`. `UnitPriceCents` stays the price of one variant, so a multi-pack can trip `OVER_UNIT_CEILING` (fail closed).
  - New in `agents`: `NewReapAdapter(reap.Client)`, `MockAdapter` (canned or synthesised offers, `Fail`/`Slow` hooks), `NewLLMParser(llm.Client)` (delegates to `llm.ExtractLineItems`), `MatchCatalog` (exact SKU, exact name/alias, fuzzy token match >= 0.6, then the LLM's SKU suggestion only if similarity >= 0.5, else off-list), `PackCount` / `UnitsPerVariant` / `PurchaseQty` / `Tokens`, `OrderBackend` + `NewTools(backend, store, audit)` (the `ToolExecutor`; tool errors come back as `{"error","code"}`, and arguments may be an object or a JSON-encoded string), `SummarizeRequest`, and `ChatAgent` (the `TextAgent`: DB prompt `agent` loaded every turn, max 6 LLM steps with the last forced to `tool_choice: none`, every message persisted in `chat_messages`).
  - `orchestrator.Deps` gained optional `Evaluate` (default `policy.Evaluate`), `MaxSearchConcurrency` (global cap on concurrent vendor searches, default 4, because the sandbox 503s above about 6), `MaxOffListVendors` (default 6), `EnrollmentPollInterval` / `EnrollmentPollTimeout`, `NewID` and `Logger`. `(*orchestrator.Impl).Tools()` returns the `agents.OrderBackend`. Exported pure helpers: `RankOffers`, `PlanSearch`, `VendorPriority`, `BuildLineItems`.
  - Orchestrator behaviour worth knowing for the UI and e2e: `New` registers its job handlers on `Deps.Queue` (call it before `Start`). The search runs as one job per request that fans out internally (preferred vendors with ONLY, then for off-list items the office vendors whose name/category/notes overlap the description, plus one open search). It ranks whatever arrived by `SEARCH_DEADLINE`. `Confirm` re-runs policy (month-to-date spend may have changed since quoting). A REJECT order goes `quoted -> rejected` at search time. Checkout reuses a still-valid quote from an earlier round, so after a price_drift approval no second quote is created. Reap POSTs use `<payment.idempotency_key>:quote` and `<payment.idempotency_key>:checkout`. The poll job reschedules itself until the request leaves `awaiting_payment`/`paying`, or until `CHECKOUT_POLL_TIMEOUT` passes (polling then stops but the request is **not** failed; `RefreshPayments` still works).
- Integrator (end-to-end pass): (1) `orchestrator.Deps.Matcher` (optional `OfferMatcher`, `LLMOfferMatcher(llm.Client)`) is wired in `cmd/api`: after allow-list dedupe and before ranking, each line's offers go through `llm.MatchOffers`; offers judged not the requested item are dropped and the model's pack size is applied. A matcher error or malformed answer keeps the unfiltered offers. Live sandbox runs previously selected Letter-size or 50-sheet colour paper for the A4 ream. (2) `domain.Offer.Description` is transient (`json:"-"`, not persisted): a short plain-text product description from Reap details (`agents.ShortDescription`), sent to the matcher because many titles are only brand or model names. `llm.OfferCandidate` and `llm.MatchInputOffer` gain `description`. (3) Seed and fixture emails moved from `@jarvis-office.example` to `@example.com`. Reap rejects reserved TLDs on enrollments (400 AGENTIC_REQUEST_REJECTED), and quotes fail with 503 QUOTE_TEMPORARILY_UNAVAILABLE when the contact email's domain does not exist in DNS. `domain.ReservedEmailDomain` now refuses reserved-TLD emails in admin address validation and `StartEnrollment`. (4) The e2e fake OpenAI answers the `offer_matches` schema. (5) New smoke test `internal/orchestrator/smoke_test.go` creates real sandbox quotes (no checkout) for every seeded address.
- Stock rejections and pre-confirm quote check (2026-10-09): Reap rejects a whole quote when one line exceeds the merchant's stock (sandbox: 409, or 400 `AGENTIC_REQUEST_REJECTED` with `detail.errors[].field = "items"`); search only exposes `available`. `reap.IsItemRejection(err)` classifies these (plus `VARIANT_UNAVAILABLE` and other 409s except quote-lifecycle codes); `APIError.FieldErrors()` reads `detail.errors`. (1) **Checkout**: an item rejection is not fatal. The merchant's items are quoted one at a time (key `<request_id>:probe:<sha256(body)>`, one per distinct body) to find the offending line(s); each moves to its next-ranked available offer at another allowed merchant that has no checkout and no quote a checkout may have been sent for. Policy is re-run (ceilings, per-order limit, monthly budget incl. committed spend), totals stored, and the round re-grouped; a basket that now needs approval opens a `policy` approval. No fallback → `failed` with a plain reason, e.g. `Popular Bookstore could not supply 20 × STAEDTLER ... (likely limited stock). Try a smaller quantity or another item.` (2) **Preflight**: while moving to `quoted`, each merchant group is quoted in parallel (default address, requester email, key `<request_id>:preflight:probe:<hash>`, `Deps.QuoteProbeTimeout` default 6s, negative disables). These quotes are never checked out or reused. A rejected line is switched the same way, or marked `REJECT` with reason code `UNAVAILABLE` ("X can't supply 10 × Y (limited stock), and no other approved vendor has it."). Other probe errors are ignored. (3) New line reason codes `VENDOR_SWITCHED` and `UNAVAILABLE` are notes that persist across policy re-runs; `agents.LineSummary.note` carries them for the voice agent. New audit types `checkout.item_rejected` and `checkout.offer_replaced`, and SSE events of the same names. Fake: `fakereap.Server.SetVariantMaxQuantity(variantID, max)`.
