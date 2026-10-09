> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Overview

> Let an agent buy on behalf of a user while every charge stays under user approval

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

Agentic Payments lets an AI agent complete a purchase for a user without handling the card number of that user.

The user stores a card once. Your agent then searches merchant catalogs and prices an order. It opens a checkout against the stored card, and the user approves the charge on a page hosted by Reap. Reap completes the purchase with the merchant and returns the order reference.

## What Reap handles

You do not need to build any of the following.

* Card storage, so the card number never reaches your agent or your servers
* Merchant catalog search, product detail lookup, and live pricing
* The hosted page where the user enters a new card
* The hosted page where the user approves each charge
* Checkout with the merchant, including the order reference that comes back

## What you build

* The agent logic that decides what to buy
* The call sequence described in this section

## Core resources

Five resources carry the whole flow. [How It Works](/agentic-payments/how-it-works) explains each one.

* An enrollment stores a card. [`POST /agentic/enrollments`](/api-reference/agentic/create-enrollment) returns the enrollment ID, which is the payment handle for every later purchase.
* A mandate records the charge terms the user approved for one enrollment. Read it with [`GET /agentic/mandates/:id`](/api-reference/agentic/get-mandate).<MandateBadge />
* Product search finds a product in merchant catalogs and resolves it to a purchasable variant. Start with [`POST /agentic/products/search`](/api-reference/agentic/search-products).
* A quote prices an order with the merchant and expires after a short window. Create it with [`POST /agentic/quotes`](/api-reference/agentic/create-quote).
* A checkout charges the enrollment and completes the order with the merchant. Create it with [`POST /agentic/checkouts`](/api-reference/agentic/create-checkout).

{!MANDATES_AVAILABLE && (
<Note>
  Mandates are not available yet. This section is published ahead of release so you can plan the integration, and the endpoints are not live in sandbox or production. Contact us for the current timeline before you build against them.
</Note>
)}

## Use cases

<CardGroup cols={1}>
  <Card title="Shopping agent">
    The user asks the agent to buy Sony WH-1000XM5 headphones for about USD 130. The agent finds them at audiohub.com and quotes USD 138.42 including shipping and tax. The user opens the approval link, checks the order, and pays with their card. The agent reports order 12345 with Friday delivery.
  </Card>
</CardGroup>

## Next steps

<CardGroup cols={2}>
  <Card title="How It Works" href="/agentic-payments/how-it-works">
    The actors, the resources, and the two phases of a purchase.
  </Card>

  <Card title="Setup" href="/agentic-payments/setup">
    Store a card and confirm it is ready to charge.
  </Card>

  <Card title="One-Time Purchases" href="/agentic-payments/one-time-purchases">
    Run a single purchase from search to a confirmed order.
  </Card>

  <Card title="Recurring Purchases" href="/agentic-payments/recurring-purchases">
    Charge the same card on a schedule under approved terms.<MandateBadge />
  </Card>
</CardGroup>

[Lifecycle and Statuses](/agentic-payments/lifecycle) covers every status value and the operations that change it. The [FAQ](/agentic-payments/faq) answers the questions that come up most during an integration.


This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.