> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Overview

> Reap API reference.

REST API with JSON request and response bodies.

## Base URLs

| Environment | Base URL |
| - | - |
| Singapore sandbox | `https://sg.sandbox.api.reap.global` |
| Singapore production | `https://sg.prod.api.reap.global` |
| Mexico sandbox | `https://mx.sandbox.api.reap.global` |
| Mexico production | `https://mx.prod.api.reap.global` |

`https://sandbox.api.reap.global` and `https://prod.api.reap.global` remain valid aliases for Singapore.

Sandbox and production are fully isolated. API keys, webhooks, and data live in one environment and never cross.

## Required Headers

Every request must include:

| Header | Description |
| - | - |
| `Authorization` | Your API key as a Bearer token: `Bearer YOUR_API_KEY` |
| `Reap-Version` | API version in YYYY-MM-DD format. Current version: `2025-02-14` |
| `Content-Type` | `application/json` for requests with a body |

## Example Request

```bash theme={null}
curl https://sg.sandbox.api.reap.global/users \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Reap-Version: 2025-02-14"
```

## Numerical Values

Monetary and rate fields use different JSON types depending on what they represent, chosen so partners never silently lose precision:

| Field shape | JSON type | Notes |
| - | - | - |
| Fiat amounts (USD, EUR, ...) | `number` | At the currency's native precision (2 decimal places for most fiat). Safe to read as a JS `number`. |
| Crypto and virtual asset amounts | `string` | Decimal string padded to the asset's `decimals` (e.g. `"1.000000"` for 1 USDC). Lossless across all values; compare and arithmetic via a big-decimal library. |
| Exchange and conversion rates | `number` | 6 decimal places. |

A field's individual description always wins when it differs. The same conventions are followed on input bodies.


This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.