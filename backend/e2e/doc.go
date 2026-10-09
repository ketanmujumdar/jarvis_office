// Package e2e holds the end-to-end suite for Jarvis Office (build tag "e2e"; run with `make e2e`).
//
// The suite builds and boots the real api binary (backend/cmd/api) against a fresh Postgres
// database (jarvis_e2e, created next to TEST_DATABASE_URL's database) and points its real Reap and
// OpenAI HTTP clients at wire-level fakes from package harness (REAP_BASE_URL / OPENAI_BASE_URL).
// It then drives everything over HTTP + SSE, like the Flutter app does:
//
//   - PRD example 1 ("printer paper and coffee pods, the usual"): parse, search, rank, policy
//     AUTO_APPROVE, address choice, Reap quote + checkout, hosted approval, ordered.
//   - PRD example 2 ("standing desk for the new hire"): off-list, NEEDS_APPROVAL with the top 3
//     quotes, approver approves, checkout completes; and a rejected variant.
//   - card enrollment through the (fake) Reap-hosted page; confirm is refused until it is ACTIVE.
//   - delivery address selection reaching the Reap quote's shippingAddress.
//   - live-quote price drift above policy (5%) sending the request back to approval.
//   - SSE event order, the per-request and global audit trail, orders and monthly spend.
//   - the voice tool relay (/agent/tools/{name}) and the text agent (/agent/chat).
//   - an admin system-prompt edit reaching the realtime session config and the text agent.
//   - no fake API key or card digit ever shows up in logs, audit rows or API responses.
//
// Real keys in backend/.env are never read: the api runs with DOTENV pointing at a missing file
// and with random throwaway keys that only the fakes accept.
package e2e
