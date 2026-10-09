> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Overview

> Real-time event notifications when resources change.

Reap sends HTTP POST requests to your endpoint whenever resources change. Use webhooks to stay in sync without polling.

## Setup

Register an endpoint by calling [Create a webhook endpoint](/api-reference/webhooks/create-webhook-endpoint) with a `name` and an HTTPS `url`. The response includes a `signingSecret` returned exactly once. Store it securely. Reap does not retain a copy, and there is no way to read it back later.

```json theme={null}
{
  "name": "production-events",
  "url": "https://api.example.com/webhooks/reap"
}
```

You can register up to 5 active endpoints per project. Every event type is delivered to every active endpoint. To stop deliveries to an endpoint, call [Disable a webhook endpoint](/api-reference/webhooks/disable-webhook-endpoint). Disabling is one-way, so create a new endpoint if you need to resume delivery to the same URL.

To rotate a signing secret (for example if the previous value leaked), call [Rotate the signing secret](/api-reference/webhooks/rotate-signing-secret). The old secret is invalidated immediately. Update your verifier with the new value before the next event arrives, or in-flight deliveries will fail HMAC verification on your endpoint and be retried automatically.

Your endpoint must return a `2xx` status code to acknowledge each delivery. Non-`2xx` responses or timeouts trigger automatic retries with exponential backoff over approximately 48 hours.

## Source IP addresses

Webhook deliveries originate from a fixed set of egress IP addresses per environment. If your endpoint sits behind a firewall or allowlist, permit inbound traffic from all of the addresses for the environment you integrate with.

| Environment | Egress IP addresses |
| - | - |
| Singapore sandbox | `13.228.84.42/32` |
| Singapore production | `13.251.96.90/32`, `13.229.17.218/32`, `3.1.46.185/32` |
| Mexico sandbox | `78.14.100.165/32` |
| Mexico production | `78.13.78.56/32`, `78.12.158.189/32`, `78.14.99.200/32` |

<Note>
  Allowlist every address listed for your environment. These addresses are stable, but we will notify you in advance if they ever change.
</Note>

## Event envelope

Every delivery wraps event data in a standard envelope:

```json theme={null}
{
  "id": "evt_01J5K7Q8R9T0V1W2X3Y4Z5A6B7",
  "type": "CRYPTO_DEPOSIT_CREATED",
  "data": {
    "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
    "accountId": "7c9e6679-7425-40de-944b-e07fc1f90ae7",
    "chainId": "BASE",
    "status": "PENDING",
    "amount": "100.00"
  }
}
```

| Field | Type | Description |
| - | - | - |
| `id` | `string` | Unique event ID. Use for idempotency. |
| `type` | `string` | Event type, e.g. `CRYPTO_DEPOSIT_CREATED`. |
| `data` | `object` | Payload matching the corresponding API resource. |

## HTTP headers

| Header | Description |
| - | - |
| `X-Reap-Webhook-Id` | Unique event ID (same as `id` in the body). |
| `X-Reap-Webhook-Timestamp` | Unix timestamp (seconds) of signature generation. |
| `X-Reap-Webhook-Signature` | HMAC-SHA256 signature. See [Signature Verification](/webhooks/signature-verification). |
| `Content-Type` | Always `application/json`. |

## Event types

| Event | Description |
| - | - |
| `USER_APPLICATION_STATUS_UPDATED` | User application status changed (KYC approval, rejection, etc.) |
| `COMPANY_STATUS_UPDATED` | Company status changed (KYB approval, rejection, etc.) |
| `CRYPTO_DEPOSIT_CREATED` | New on-chain deposit detected |
| `CRYPTO_DEPOSIT_STATUS_UPDATED` | Deposit status changed (approved, rejected, etc.) |
| `CARD_STATUS_UPDATED` | Card status changed (activated, frozen, blocked, etc.) |
| `ACCOUNT_STATUS_UPDATED` | Account status changed (restricted, reactivated, etc.) |
| `CARD_TRANSACTION_CREATED` | New card transaction |
| `CARD_TRANSACTION_UPDATED` | Card transaction lifecycle event (clearing, reversal, refund, dispute refund, etc.) |
| `CARD_3DS_CHALLENGE_CREATED` | 3DS challenge created for a cardholder (see [3D Secure](/cards/3d-secure)) |
| `CARD_SHIPMENT_STATUS_UPDATED` | Physical card shipment status changed |
| `CARD_TOKENIZATION_REQUESTED` | OTP for adding a card to a digital wallet via SMS verification (valid for a short window) |
| `CARD_FRAUD_ALERT_CREATED` | Fraud alert created (detected or reported). See [Fraud Alerts](/cards/fraud-alerts) |
| `CARD_FRAUD_ALERT_STATUS_UPDATED` | Fraud alert status changed (confirmed, declined, or expired) |
| `CARD_DISPUTE_STATUS_UPDATED` | Card dispute status changed |

<Note>
  Payloads match the shape of the corresponding API resource. For example, `CRYPTO_DEPOSIT_CREATED` data matches `GET /crypto-deposits/:id`.
</Note>

## Delivery guarantees

Every event is delivered **at least once**, and **in no particular order**.

A delivery that times out or returns a non-`2xx` status is retried with exponential backoff over approximately 48 hours. A response we never see looks the same to us as a failure, so an event you already processed can arrive again. Deduplicate on the envelope `id`, which is stable across every redelivery.

Ordering is not guaranteed for any pair of events, including two events about the same resource. Treat each delivery as a statement about one resource at a point in time rather than a step in a sequence: upsert on the resource id in `data`, and ignore a payload whose `updatedAt` is older than what you have already stored.

## Best practices

* **Return quickly.** Respond with `2xx` within a few seconds. Process heavy work in the background.
* **Be idempotent.** Events may be delivered more than once. Use the `id` field to skip duplicates.
* **Do not rely on ordering.** Reconcile from the payload rather than the arrival sequence.
* **Verify signatures.** Always [verify the signature](/webhooks/signature-verification) before processing.


This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.