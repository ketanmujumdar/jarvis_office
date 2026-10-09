> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Authentication

> How to authenticate API requests to Reap.

All API requests require authentication using an API key.

## API Key

Include your API key as a Bearer token in the `Authorization` header:

```
Authorization: Bearer YOUR_API_KEY
```

API keys are project-scoped. Each key grants access to resources within a single project.

## API Versioning

Include the `Reap-Version` header in every request to specify which API version to use:

```
Reap-Version: 2025-02-14
```

The version uses a date-based format (YYYY-MM-DD). Requests without this header will be rejected.

## Example

```bash theme={null}
curl https://sg.sandbox.api.reap.global/users \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Reap-Version: 2025-02-14"
```

## Environments

| Environment | Base URL |
| - | - |
| Singapore sandbox | `https://sg.sandbox.api.reap.global` |
| Singapore production | `https://sg.prod.api.reap.global` |
| Mexico sandbox | `https://mx.sandbox.api.reap.global` |
| Mexico production | `https://mx.prod.api.reap.global` |

`https://sandbox.api.reap.global` and `https://prod.api.reap.global` remain valid aliases for Singapore.

API keys are scoped to a single environment. A sandbox key cannot be used against production and vice versa.

## IP allowlist

Each key can optionally be restricted to a list of IPv4/IPv6 addresses or CIDR ranges. An empty list means the key works from any IP.

When a list is set, requests from any other address are rejected with `403` and code `API_KEY_IP_NOT_ALLOWED`. Reap uses the last `X-Forwarded-For` hop (the address the load balancer saw). If that hop is missing, the request is also rejected.

<Note>
  Contact the Reap team to obtain API keys for your project.
</Note>


This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.