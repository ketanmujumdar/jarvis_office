---
name: jarvis-mvp-decisions
description: Agreed MVP scope cuts and Reap sandbox facts for Jarvis Office procurement agent (decided 2026-10-09)
metadata:
  node_type: memory
  type: project
  originSessionId: 1048c43a-3aac-4448-a205-d8dcba203f6b
  modified: 2026-10-09T08:57:18.648Z
---

MVP decisions (2026-10-09), deviating from docs/PRD.md:
- Single Go `api` service; no NATS (goroutines behind a queue interface). No scraper/vendor-API workers; all search via Reap `/agentic/products/search`.
- Currency SGD, country SG. Reap base `https://sg.sandbox.api.reap.global`, header `Reap-Version: 2025-02-14`, Bearer auth. Sandbox returns real SG merchants (Popular Bookstore, etc.).
- Reap mandates NOT live yet; user approval = Reap-hosted approval page from `POST /agentic/checkouts`, then poll `GET /agentic/checkouts/:id`. Card stored via `EXTERNAL` enrollment hosted page — never store card data ourselves.
- Approval expiry removed. Admin page needed with fake login (no real auth). UI must look professional/modern.
- Admin seeds multiple Singapore delivery addresses; agent asks which address at confirmation.
- Agent system prompt must be editable (stored in DB, editable in admin).
- Flutter web first; keys in backend/.env (gitignored).

**Why:** hackathon demo; user wants one-shot multi-agent build with solid tests.
**How to apply:** follow these over the PRD where they conflict.

## Additional (2026-10-09)
- Merchants: ONLY those in backend/seed/allowed_merchants.tsv (domains). Reap search returns `merchant.name` only (e.g. "Common Man Coffee Roasters SG"), so each vendor row needs a `reap_merchant_name` resolved by probing the sandbox; filter offers to allowlisted merchants; use `merchantPreference {mode: ONLY|PREFER, merchantName}` for preferred vendors.
- Checkout status via polling `GET /agentic/checkouts/:id` (webhook receiver optional, behind config, for later ngrok use).
- Reap docs snapshot: docs/reap/*.md (authoritative over PRD).
- Seed catalog (~30 items) must only reference items purchasable at allowlisted merchants (paper/stationery: popular.com.sg, kinokuniya; coffee/tea: commonman, bettr, alchemist, gryphon...; snacks: boxgreen, camelnuts, irvin; office: ergotune, picketandrail; electronics: anker, ugreen, prismplus; cleaning/household: metro, shoppy, intertech-hardware).
