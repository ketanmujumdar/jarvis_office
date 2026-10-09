> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Setup

> Store a card once and confirm that it is ready to charge

<Warning>
  Agentic Payments enrolls only cards that support Cloud Token Framework and Visa Token Service tokenization. Check with your card issuer or BIN sponsor before you create an enrollment. The [Visa fact sheet](https://usa.visa.com/content/dam/VCOM/global/products/documents/visa-vts-cloud-token-framework-fact-sheet.pdf) covers what the framework provides.
</Warning>

## Before you start

* A Reap API key for the sandbox or production environment
* Agentic Payments enabled on your project
* An HTTPS return URL that receives the user after the hosted card entry page and the hosted approval page

Every request takes an [`Authorization`](/api-reference/authentication) header and a [`Reap-Version`](/api-reference/versioning) header. The examples below use the sandbox host. Switch to the production host when you go live.

## Store a card

<Steps>
  <Step title="Create the enrollment">
    Pick the source that matches the card you are storing. [`POST /agentic/enrollments`](/api-reference/agentic/create-enrollment) requires an [`Idempotency-Key`](/api-reference/idempotency). A retry with the same key replays the first response instead of storing the card twice.

    Supply `presentation.returnUrl` to return the browser to your application after the hosted step. Presentation is required for every source. Reusing an idempotency key with a changed return URL is rejected.

    <Tabs>
      <Tab title="Reap card">
        Use this source for a card Reap already issued.

        ```bash Request theme={null}
        curl --request POST \
          --url https://sandbox.api.reap.global/agentic/enrollments \
          --header 'Authorization: Bearer <token>' \
          --header 'Content-Type: application/json' \
          --header 'Idempotency-Key: <idempotency-key>' \
          --header 'Reap-Version: <reap-version>' \
          --data '{
            "source": "REAP_CARD",
            "cardId": "<card-id>",
            "presentation": {
              "type": "REDIRECT",
              "returnUrl": "https://example.com/cards/added"
            }
          }'
        ```

        ```json Response theme={null}
        {
          "id": "<enrollment-id>",
          "status": "REQUIRES_ACTION",
          "source": "REAP_CARD",
          "owner": {
            "type": "REAP_USER",
            "id": "<user-id>"
          },
          "nextAction": {
            "type": "REDIRECT",
            "url": "<hosted-device-binding-url>"
          }
        }
        ```

        The `owner` block names the cardholder that Reap resolved from the card.
      </Tab>

      <Tab title="BIN sponsor card">
        Use this source for a card issued by a BIN sponsor.

        ```bash Request theme={null}
        curl --request POST \
          --url https://sandbox.api.reap.global/agentic/enrollments \
          --header 'Authorization: Bearer <token>' \
          --header 'Content-Type: application/json' \
          --header 'Idempotency-Key: <idempotency-key>' \
          --header 'Reap-Version: <reap-version>' \
          --data '{
            "source": "BIN_SPONSOR",
            "cardId": "<card-id>",
            "owner": {
              "type": "CLIENT_REFERENCE",
              "id": "<customer-id>",
              "name": "Customer Name",
              "email": "customer@example.com"
            },
            "presentation": {
              "type": "REDIRECT",
              "returnUrl": "https://example.com/cards/added"
            }
          }'
        ```

        ```json Response theme={null}
        {
          "id": "<enrollment-id>",
          "status": "REQUIRES_ACTION",
          "source": "BIN_SPONSOR",
          "owner": {
            "type": "CLIENT_REFERENCE",
            "id": "<customer-id>",
            "name": "Customer Name",
            "email": "customer@example.com"
          },
          "nextAction": {
            "type": "REDIRECT",
            "url": "<hosted-device-binding-url>"
          }
        }
        ```

        Supply your customer reference and the cardholder's name in the request. Include the cardholder's email. The response returns the same owner details.
      </Tab>

      <Tab title="External card">
        Use this source when the user enters a new card. Send the owner reference from your own system and the URL the user returns to.

        ```bash Request theme={null}
        curl --request POST \
          --url https://sandbox.api.reap.global/agentic/enrollments \
          --header 'Authorization: Bearer <token>' \
          --header 'Content-Type: application/json' \
          --header 'Idempotency-Key: <idempotency-key>' \
          --header 'Reap-Version: <reap-version>' \
          --data '{
            "source": "EXTERNAL",
            "owner": {
              "type": "CLIENT_REFERENCE",
              "id": "<your-customer-id>",
              "email": "jsmith@example.com"
            },
            "presentation": {
              "type": "REDIRECT",
              "returnUrl": "https://example.com/cards/added"
            }
          }'
        ```

        ```json Response theme={null}
        {
          "id": "<enrollment-id>",
          "status": "REQUIRES_ACTION",
          "source": "EXTERNAL",
          "owner": {
            "type": "CLIENT_REFERENCE",
            "id": "<your-customer-id>",
            "email": "jsmith@example.com"
          },
          "nextAction": {
            "type": "REDIRECT",
            "url": "<hosted-card-entry-url>",
            "expiresAt": "2026-01-01T00:00:00Z"
          }
        }
        ```

        Keep the enrollment ID. You need it in the next two steps and at every checkout.
      </Tab>
    </Tabs>
  </Step>

  <Step title="Send the user to the hosted enrollment page">
    This step applies to all sources. Redirect the user to `nextAction.url` before `expiresAt` passes. External cards require card entry; Reap and BIN sponsor cards require device binding. Card details never reach your servers.

    After the hosted step, Reap sends the user back to `presentation.returnUrl`. Returning to your URL does not prove the card is ready, so confirm the status next.
  </Step>

  <Step title="Confirm the enrollment is ACTIVE">
    Read the enrollment with [`GET /agentic/enrollments/:id`](/api-reference/agentic/get-enrollment) after any hosted step.

    ```bash Request theme={null}
    curl --request GET \
      --url https://sandbox.api.reap.global/agentic/enrollments/:id \
      --header 'Authorization: Bearer <token>' \
      --header 'Reap-Version: <reap-version>'
    ```

    ```json Response theme={null}
    {
      "id": "<enrollment-id>",
      "status": "ACTIVE",
      "owner": {
        "type": "REAP_USER",
        "id": "<user-id>",
        "email": "jsmith@example.com"
      },
      "paymentMethod": {
        "type": "CARD",
        "network": "<network>",
        "last4": "4242",
        "expiryMonth": 11,
        "expiryYear": 2029
      },
      "nextAction": null,
      "createdAt": "2026-01-01T00:00:00Z",
      "updatedAt": "2026-01-01T00:00:00Z"
    }
    ```

    A status of `ACTIVE` means the card is ready to charge. A status of `REQUIRES_ACTION` means a hosted step is still outstanding, and `nextAction` tells you where to send the user. The `paymentMethod` block gives you the network and the last four digits to display in your own interface.
  </Step>
</Steps>

<Warning>
  You can charge only an `ACTIVE` enrollment. Read the enrollment before you open a checkout rather than assuming the hosted page succeeded.
</Warning>

## Present the available enrollment cards to the user

List the enrollments of one owner with [`GET /agentic/enrollments`](/api-reference/agentic/list-enrollments) and show each stored card so the user can pick one. The `paymentMethod` block carries the network, the last four digits, and the expiry to display. Scope every list call to a single owner.

```bash Request theme={null}
curl --request GET \
  --url 'https://sandbox.api.reap.global/agentic/enrollments?ownerType=REAP_USER&ownerId=:id&limit=20' \
  --header 'Authorization: Bearer <token>' \
  --header 'Reap-Version: <reap-version>'
```

```json Response theme={null}
{
  "items": [
    {
      "id": "<enrollment-id>",
      "status": "ACTIVE",
      "owner": {
        "type": "REAP_USER",
        "id": "<user-id>",
        "email": "jsmith@example.com"
      },
      "paymentMethod": {
        "type": "CARD",
        "network": "<network>",
        "last4": "4242",
        "expiryMonth": 11,
        "expiryYear": 2029
      },
      "nextAction": null,
      "createdAt": "2026-01-01T00:00:00Z",
      "updatedAt": "2026-01-01T00:00:00Z"
    }
  ],
  "nextCursor": null
}
```

Follow `nextCursor` when it holds a value. A `null` cursor means the last page.

## Test cards

Use these test cards on the hosted card entry page to run the full payment flow in the sandbox. They behave like real cards but never move money. Reap adds more cards and networks as it supports them.

<Note>
  These cards only work through the Agentic Payments API and are declined everywhere else.
</Note>

| Card number | CVC | Expiry |
| - | - | - |
| 4622 9431 2313 7797 | 640 | 12/27 |
| 4622 9431 2313 7805 | 304 | 12/27 |
| 4622 9431 2313 7847 | 698 | 12/27 |

## One-time passwords

The card verification step may ask for a one-time password. Enter `456789`. The same code works whether you choose email or SMS delivery.

## Next steps

<CardGroup cols={2}>
  <Card title="One-Time Purchases" href="/agentic-payments/one-time-purchases">
    Run a single purchase against the enrollment you created.
  </Card>

  <Card title="Lifecycle and Statuses" href="/agentic-payments/lifecycle">
    Revoke a stored card and read every status value.
  </Card>
</CardGroup>

Field level detail for every field, header, and error sits in the [API Reference](/api-reference/overview) tab. This page carries the sequence rather than repeating that detail.


This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.