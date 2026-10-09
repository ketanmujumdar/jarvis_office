> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# One-Time Purchases

> Run a single agentic purchase from product search to a confirmed order

export const MandateBadge = ({label = "Coming soon"}) => MANDATES_AVAILABLE ? null : <span style={{
  display: "inline-block",
  verticalAlign: "middle",
  marginLeft: "0.4em",
  padding: "0.15em 0.65em",
  border: "1px solid rgba(113, 247, 236, 0.45)",
  borderRadius: "9999px",
  backgroundColor: "rgba(113, 247, 236, 0.12)",
  color: "#71F7EC",
  fontSize: "0.7em",
  fontWeight: 600,
  lineHeight: 1.7,
  letterSpacing: "0.02em",
  whiteSpace: "nowrap"
}}>
      {label}
    </span>;

export const MANDATES_AVAILABLE = false;

This page runs one purchase end to end. You start with a stored card and finish with a merchant order reference and the amount charged.

## Before you start

You need an enrollment with status `ACTIVE`. [Setup](/agentic-payments/setup) covers how to create one, and [`GET /agentic/enrollments/:id`](/api-reference/agentic/get-enrollment) reads its status.

```bash Confirm the enrollment theme={null}
curl --request GET \
  --url https://sandbox.api.reap.global/agentic/enrollments/:id \
  --header 'Authorization: Bearer <token>' \
  --header 'Reap-Version: <reap-version>'
```

Continue when `status` reads `ACTIVE`. Anything else means you cannot charge the card yet.

## Check the mandate<MandateBadge />

{!MANDATES_AVAILABLE && (
<Note>
  Mandates are not available yet. This section is published ahead of release so you can plan the integration, and the endpoints are not live in sandbox or production. Contact us for the current timeline before you build against them.
</Note>
)}

A single charge runs under a mandate with `frequency` set to `ONE_TIME` and `maxCharges` set to 1. Read the mandate with [`GET /agentic/mandates/:id`](/api-reference/agentic/get-mandate) to confirm the terms before you price anything.

```bash Request theme={null}
curl --request GET \
  --url https://sandbox.api.reap.global/agentic/mandates/:id \
  --header 'Authorization: Bearer <token>' \
  --header 'Reap-Version: <reap-version>'
```

```json Response theme={null}
{
  "id": "<mandate-id>",
  "status": "ACTIVE",
  "enrollmentId": "<enrollment-id>",
  "frequency": "ONE_TIME",
  "maxCharges": 1,
  "merchantScope": "LISTED",
  "merchant": {
    "name": "<merchant-name>",
    "url": "https://example.com",
    "country": "US"
  },
  "amount": {
    "amount": 150,
    "currency": "USD"
  }
}
```

The `amount` is a ceiling rather than a target. Compare it against the final quote total once you select a shipping option. A one-time mandate authorizes a single charge and cannot authorize another once that charge completes.

## Run the purchase

<Steps>
  <Step title="Search merchant catalogs">
    Send the words the user gave you to [`POST /agentic/products/search`](/api-reference/agentic/search-products). Filters narrow the result set, and `context` sets the country and currency for pricing.

    If your application handles discovery, skip the search and variant steps. Construct a merchant checkout URL with the item quantity and any attribution parameters. Then continue at **Create the quote**.

    ```bash Request theme={null}
    curl --request POST \
      --url https://sandbox.api.reap.global/agentic/products/search \
      --header 'Authorization: Bearer <token>' \
      --header 'Content-Type: application/json' \
      --header 'Reap-Version: <reap-version>' \
      --data '{
        "query": "Sony WH 1000XM5 headphones",
        "context": {
          "country": "US",
          "currency": "USD"
        },
        "filters": {
          "price": {
            "min": "100",
            "max": "200"
          },
          "availability": "AVAILABLE_ONLY"
        },
        "pagination": {
          "limit": 20
        }
      }'
    ```

    ```json Response theme={null}
    {
      "id": "<search-id>",
      "products": [
        {
          "id": "<product-id>",
          "merchant": {
            "name": "<merchant-name>"
          },
          "name": "Sony WH 1000XM5 Wireless Headphone",
          "imageUrl": "https://example.com/image.jpg",
          "priceRange": {
            "min": { "amount": 129, "currency": "USD" },
            "max": { "amount": 149, "currency": "USD" }
          },
          "available": true,
          "previewVariant": {
            "id": "<variant-id>",
            "name": "<variant-name>",
            "price": { "amount": 129, "currency": "USD" },
            "available": true
          }
        }
      ],
      "pagination": {
        "nextCursor": "<cursor>",
        "hasNextPage": true,
        "returnedCount": 20
      },
      "warnings": []
    }
    ```

    Shortlist a product by `id`. Add `merchantPreference` to the request when the user names a merchant or when you want Reap to prefer one.
  </Step>

  <Step title="Read the product details">
    Search results carry a price range and one preview variant. [`POST /agentic/products/details`](/api-reference/agentic/get-product-details) expands a product into its description, media, and option groups.

    ```bash Request theme={null}
    curl --request POST \
      --url https://sandbox.api.reap.global/agentic/products/details \
      --header 'Authorization: Bearer <token>' \
      --header 'Content-Type: application/json' \
      --header 'Reap-Version: <reap-version>' \
      --data '{
        "productIds": ["<product-id>"]
      }'
    ```

    ```json Response theme={null}
    {
      "products": [
        {
          "id": "<product-id>",
          "name": "Sony WH 1000XM5 Wireless Headphones",
          "options": [
            {
              "name": "Color",
              "values": [
                { "optionId": "<option-id>", "label": "Black", "available": true },
                { "optionId": "<option-id>", "label": "Silver", "available": false }
              ]
            }
          ],
          "defaultVariant": {
            "id": "<variant-id>",
            "price": { "amount": 129, "currency": "USD" },
            "available": true,
            "requiresShipping": true
          }
        }
      ],
      "errors": []
    }
    ```

    Carry the `optionId` of each chosen value into the next step. Check `available` on every option value, because an unavailable value cannot resolve to a purchasable variant. Any product ID that fails to resolve comes back in `errors` rather than stopping the call.

    Skip the next step when the user accepts the `defaultVariant`, which is already a purchasable variant.
  </Step>

  <Step title="Resolve the variant">
    A quote accepts a variant rather than a product. Turn the selected options into one variant with [`POST /agentic/products/variant`](/api-reference/agentic/resolve-variant).

    ```bash Request theme={null}
    curl --request POST \
      --url https://sandbox.api.reap.global/agentic/products/variant \
      --header 'Authorization: Bearer <token>' \
      --header 'Content-Type: application/json' \
      --header 'Reap-Version: <reap-version>' \
      --data '{
        "productId": "<product-id>",
        "optionIds": ["<option-id>"]
      }'
    ```

    ```json Response theme={null}
    {
      "id": "<variant-id>",
      "name": "<variant-name>",
      "options": [
        { "name": "Color", "value": "Black" }
      ],
      "price": { "amount": 129, "currency": "USD" },
      "available": true,
      "requiresShipping": true
    }
    ```

    A `requiresShipping` value of `true` means the quote needs a shipping address. Stop here when `available` reads `false` and offer the user another option.
  </Step>

  <Step title="Create the quote">
    [`POST /agentic/quotes`](/api-reference/agentic/create-quote) opens the merchant checkout and returns current pricing from the merchant. Send exactly one of `items` and `externalCheckout`. `offerCode` is optional for both quote sources.

    <Tabs>
      <Tab title="Reap discovery">
        Use `items` with the variant IDs returned by Reap discovery. Send the shipping address whenever a variant requires shipping.

        ```bash Request theme={null}
        curl --request POST \
          --url https://sandbox.api.reap.global/agentic/quotes \
          --header 'Authorization: Bearer <token>' \
          --header 'Content-Type: application/json' \
          --header 'Idempotency-Key: <idempotency-key>' \
          --header 'Reap-Version: <reap-version>' \
          --data '{
            "items": [{ "variantId": "var_123", "quantity": 1 }],
            "email": "avery.tan@reap.hk",
            "offerCode": "SAVE10"
          }'
        ```
      </Tab>

      <Tab title="Checkout URL">
        Use `externalCheckout` with the checkout URL your application constructed. Reap sends the URL to the merchant without rebuilding it, so attribution parameters remain intact. External checkout quotes require a shipping address.

        <Note>
          External checkout URLs are available only for merchant domains that Reap has allowlisted for your integration. Contact Reap to request access before you send checkout URLs for a new merchant.
        </Note>

        ```bash Request theme={null}
        curl --request POST \
          --url https://sandbox.api.reap.global/agentic/quotes \
          --header 'Authorization: Bearer <token>' \
          --header 'Content-Type: application/json' \
          --header 'Idempotency-Key: <idempotency-key>' \
          --header 'Reap-Version: <reap-version>' \
          --data '{
            "externalCheckout": {
              "merchantDomain": "merchant.example",
              "checkoutUrl": "https://merchant.example/cart/variant-1:1?attributes[partner_click_id]=example-123"
            },
            "email": "avery.tan@reap.hk",
            "shippingAddress": {
              "firstName": "Avery",
              "lastName": "Tan",
              "phone": "+85200000000",
              "addressLine1": "123 Example Street",
              "city": "Example City",
              "postalCode": "000000",
              "country": "HK"
            },
            "offerCode": "SAVE10"
          }'
        ```
      </Tab>
    </Tabs>

    ```json Response theme={null}
    {
      "id": "<quote-id>",
      "shippingOptions": [
        {
          "id": "<standard-option-id>",
          "name": "Standard",
          "selected": false,
          "price": { "amount": 5, "currency": "USD" }
        },
        {
          "id": "<express-option-id>",
          "name": "Express",
          "selected": true,
          "price": { "amount": 13, "currency": "USD" }
        }
      ],
      "amountBreakdown": {
        "itemsSubtotal": { "amount": 129, "currency": "USD" },
        "shipping": { "amount": 13, "currency": "USD" },
        "tax": {
          "amount": { "amount": 5, "currency": "USD" },
          "includedInPrices": false
        },
        "discounts": [],
        "additionalCharges": [],
        "finalAmount": { "amount": 142, "currency": "USD" }
      },
      "expiresAt": "2026-01-01T00:00:00Z"
    }
    ```

    Keep the quote ID and note `expiresAt`. Prices come from the merchant, so the total can differ from the variant price once shipping and tax land.

    Reap makes one merchant request for each quote attempt. It does not retry a failed request.

    Correct `CHECKOUT_URL_INVALID`, `OFFER_CODE_INVALID`, `OFFER_CODE_EXPIRED`, and `QUOTE_UNFULFILLABLE` errors before you create another quote. `CARD_PAYMENT_UNAVAILABLE` means card payment is unavailable for this checkout. Do not retry the same request unchanged; choose another checkout or contact Reap.
  </Step>

  <Step title="Select a shipping option">
    One option arrives preselected. Send a different one to [`POST /agentic/quotes/:id/shipping-option`](/api-reference/agentic/select-shipping-option), which reprices the quote and returns the updated breakdown.

    ```bash Request theme={null}
    curl --request POST \
      --url https://sandbox.api.reap.global/agentic/quotes/:id/shipping-option \
      --header 'Authorization: Bearer <token>' \
      --header 'Content-Type: application/json' \
      --header 'Reap-Version: <reap-version>' \
      --data '{
        "shippingOptionId": "<standard-option-id>"
      }'
    ```

    ```json Response theme={null}
    {
      "id": "<quote-id>",
      "shippingOptions": [
        {
          "id": "<standard-option-id>",
          "name": "Standard",
          "selected": true,
          "price": { "amount": 5, "currency": "USD" }
        },
        {
          "id": "<express-option-id>",
          "name": "Express",
          "selected": false,
          "price": { "amount": 13, "currency": "USD" }
        }
      ],
      "amountBreakdown": {
        "itemsSubtotal": { "amount": 129, "currency": "USD" },
        "shipping": { "amount": 5, "currency": "USD" },
        "tax": {
          "amount": { "amount": 5, "currency": "USD" },
          "includedInPrices": false
        },
        "finalAmount": { "amount": 134, "currency": "USD" }
      },
      "expiresAt": "2026-01-01T00:00:00Z"
    }
    ```

    Show `amountBreakdown.finalAmount` to the user as the amount you will charge. Read the quote again with [`GET /agentic/quotes/:id`](/api-reference/agentic/get-quote) at any point to get the current total without changing anything.
  </Step>

  <Step title="Create the checkout">
    [`POST /agentic/checkouts`](/api-reference/agentic/create-checkout) charges the enrollment for the quote. Send the URL the user returns to after approving. In the sandbox, send `X-Simulate-Checkout: COMPLETED` to simulate a completed checkout. This header is rejected in production.

    ```bash Request theme={null}
    curl --request POST \
      --url https://sandbox.api.reap.global/agentic/checkouts \
      --header 'Authorization: Bearer <token>' \
      --header 'Content-Type: application/json' \
      --header 'Idempotency-Key: <idempotency-key>' \
      --header 'Reap-Version: <reap-version>' \
      --header 'X-Simulate-Checkout: COMPLETED' \
      --data '{
        "quoteId": "<quote-id>",
        "enrollmentId": "<enrollment-id>",
        "presentation": {
          "type": "REDIRECT",
          "returnUrl": "https://example.com/orders/done"
        }
      }'
    ```

    ```json Response theme={null}
    {
      "id": "<checkout-id>",
      "status": "REQUIRES_ACTION",
      "quoteId": "<quote-id>",
      "enrollmentId": "<enrollment-id>",
      "amount": { "amount": 134, "currency": "USD" },
      "nextAction": {
        "type": "REDIRECT",
        "url": "<hosted-approval-url>",
        "expiresAt": "2026-01-01T00:00:00Z"
      }
    }
    ```

    Branch on `nextAction`. When it holds a value, send the user to `nextAction.url` to review and approve the charge. When it is empty, the charge already ran under terms the user approved earlier.
  </Step>

  <Step title="Confirm the order">
    Read the checkout with [`GET /agentic/checkouts/:id`](/api-reference/agentic/get-checkout) once the user lands back on your return URL. A completed checkout carries the merchant order reference and the amount charged.

    ```bash Request theme={null}
    curl --request GET \
      --url https://sandbox.api.reap.global/agentic/checkouts/:id \
      --header 'Authorization: Bearer <token>' \
      --header 'Reap-Version: <reap-version>'
    ```

    ```json Response theme={null}
    {
      "id": "<checkout-id>",
      "status": "COMPLETED",
      "quoteId": "<quote-id>",
      "enrollmentId": "<enrollment-id>",
      "orderId": "<merchant-order-id>",
      "finalAmount": { "amount": 134, "currency": "USD" },
      "nextAction": null,
      "createdAt": "2026-01-01T00:00:00Z",
      "updatedAt": "2026-01-01T00:00:00Z"
    }
    ```

    Store `orderId` against your own record of the purchase. It is the reference the user and the merchant both recognize. Reconcile against `finalAmount` rather than the quote total, because it is the amount actually charged.
  </Step>
</Steps>

## Next steps

<CardGroup cols={2}>
  <Card title="Recurring Purchases" href="/agentic-payments/recurring-purchases">
    Charge the same card again on a schedule.<MandateBadge />
  </Card>

  <Card title="Lifecycle and Statuses" href="/agentic-payments/lifecycle">
    Every status value and what to do about it.
  </Card>
</CardGroup>

Field level detail for every field, header, and error sits in the [API Reference](/api-reference/overview) tab. This page carries the sequence rather than repeating that detail.


This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.