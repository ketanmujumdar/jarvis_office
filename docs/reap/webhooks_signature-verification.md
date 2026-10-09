> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Signature Verification

> Verify webhook requests originate from Reap.

Every webhook is signed with HMAC-SHA256 using your project's signing secret. Verify the signature before processing any event.

## Signature format

The `X-Reap-Webhook-Signature` header looks like this:

```
t=1709312400,v1=5257a869e7ecebeda32affa62cdca3fa51cad7e77a0e56ff536d0ce8e108d8f9
```

`t` is a Unix timestamp (seconds). `v1` is the hex-encoded HMAC-SHA256 signature.

## How to verify

<Steps>
  <Step title="Parse the header">
    Split `X-Reap-Webhook-Signature` on `,`. Extract the `t=` timestamp and `v1=` signature.
  </Step>

  <Step title="Build the signed payload">
    Concatenate timestamp and raw request body with a `.` separator: `{timestamp}.{raw_body}`

    <Warning>
      Use the raw request body, not a re-serialized version. Re-stringifying JSON changes whitespace and key order, which breaks verification.
    </Warning>
  </Step>

  <Step title="Compare signatures">
    Compute HMAC-SHA256 of the signed payload using your secret. Compare the hex digest to `v1` using constant-time comparison.
  </Step>

  <Step title="Check timestamp freshness">
    Reject requests where the timestamp is more than 5 minutes old to prevent replay attacks.
  </Step>
</Steps>

## JavaScript example

```javascript theme={null}
import { createHmac, timingSafeEqual } from "crypto";

const WEBHOOK_SECRET = process.env.REAP_WEBHOOK_SECRET;
const TOLERANCE_SECONDS = 300;

function verifyWebhook(rawBody, signatureHeader) {
  const parts = signatureHeader.split(",");
  let timestamp;
  let signature;

  for (const part of parts) {
    const [key, value] = part.split("=", 2);
    if (key === "t") timestamp = parseInt(value, 10);
    if (key === "v1") signature = value;
  }

  if (!timestamp || !signature) {
    throw new Error("Invalid signature header");
  }

  const now = Math.floor(Date.now() / 1000);
  if (Math.abs(now - timestamp) > TOLERANCE_SECONDS) {
    throw new Error("Timestamp outside tolerance");
  }

  const signedPayload = `${timestamp}.${rawBody}`;
  const expected = createHmac("sha256", WEBHOOK_SECRET)
    .update(signedPayload)
    .digest("hex");

  const expectedBuf = Buffer.from(expected, "hex");
  const signatureBuf = Buffer.from(signature, "hex");

  if (
    expectedBuf.length !== signatureBuf.length ||
    !timingSafeEqual(expectedBuf, signatureBuf)
  ) {
    throw new Error("Invalid signature");
  }

  return JSON.parse(rawBody);
}
```

## Express.js example

Capture the raw body before JSON parsing:

```javascript theme={null}
import express from "express";

const app = express();

app.use("/webhooks", express.raw({ type: "application/json" }));

app.post("/webhooks", (req, res) => {
  try {
    const event = verifyWebhook(
      req.body.toString("utf-8"),
      req.headers["x-reap-webhook-signature"]
    );
    // process event
    res.sendStatus(200);
  } catch (err) {
    res.status(400).json({ error: err.message });
  }
});
```

## Security notes

* Always use constant-time comparison (`timingSafeEqual`). String equality leaks timing information.
* 5 minutes is the recommended timestamp tolerance.
* Keep your signing secret safe. If it leaks, rotate it via [Rotate the signing secret](/api-reference/webhooks/rotate-signing-secret). The previous value is invalidated immediately.


This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.