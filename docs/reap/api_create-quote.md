> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Create quote

> Opens and prices a merchant checkout from product variants or a checkout URL assembled by the client. Send exactly one of `items` and `externalCheckout`. Prices always come from the merchant. The response itemizes the total and lists the available shipping options. Quotes are short-lived: create the checkout before `expiresAt` passes, or open a fresh quote. `shippingAddress` is required for external checkout quotes and when any item requires shipping. Use `offerCode` to price an offer before approval. Reap makes one quote attempt and does not retry a failed merchant request. The error code states whether to correct the request or create a new quote.



## OpenAPI

````yaml /api-reference/openapi.json post /agentic/quotes
openapi: 3.1.0
info:
  title: Reap API
  version: 1.0.0
  description: Reap platform API
servers:
  - url: https://sg.sandbox.api.reap.global
    description: Singapore sandbox
  - url: https://sg.prod.api.reap.global
    description: Singapore production
  - url: https://mx.sandbox.api.reap.global
    description: Mexico sandbox
  - url: https://mx.prod.api.reap.global
    description: Mexico production
  - url: https://sandbox.api.reap.global
    description: Singapore sandbox (alias)
  - url: https://prod.api.reap.global
    description: Singapore production (alias)
security:
  - bearerAuth: []
tags:
  - name: Accounts
  - name: Activities
  - name: Card Designs
  - name: Card Shipments
  - name: Card Transactions
  - name: Cards
  - name: Companies
  - name: Crypto Deposits
  - name: Crypto Withdrawals
  - name: Disputes
  - name: Fees
  - name: Fiat Deposits
  - name: Fraud Alerts
  - name: Policies
  - name: Simulation
  - name: Statements
  - name: Users
  - name: Virtual Asset Postings
  - name: Virtual Assets
  - name: Webhooks
  - name: Agentic
paths:
  /agentic/quotes:
    post:
      tags:
        - Agentic
      summary: Create quote
      description: >-
        Opens and prices a merchant checkout from product variants or a checkout
        URL assembled by the client. Send exactly one of `items` and
        `externalCheckout`. Prices always come from the merchant. The response
        itemizes the total and lists the available shipping options. Quotes are
        short-lived: create the checkout before `expiresAt` passes, or open a
        fresh quote. `shippingAddress` is required for external checkout quotes
        and when any item requires shipping. Use `offerCode` to price an offer
        before approval. Reap makes one quote attempt and does not retry a
        failed merchant request. The error code states whether to correct the
        request or create a new quote.
      operationId: createQuote_agentic
      parameters:
        - name: Reap-Version
          in: header
          required: true
          schema:
            type: string
            enum:
              - '2025-02-14'
            description: API version (YYYY-MM-DD)
            example: '2025-02-14'
        - name: Idempotency-Key
          in: header
          required: true
          schema:
            type: string
            minLength: 1
            maxLength: 255
            description: >-
              Unique key for safely retrying this request. Up to 255 characters;
              we recommend a UUIDv4. The first request executes and its response
              is cached; subsequent requests with the same key replay the cached
              response (24h retention).
      requestBody:
        description: Create and price a quote. The request shape depends on its source.
        required: true
        content:
          application/json:
            schema:
              examples:
                - items:
                    - variantId: var_123
                      quantity: 1
                  email: lin.example@reap.hk
                  offerCode: SAVE10
                - externalCheckout:
                    merchantDomain: checkout.example
                    checkoutUrl: >-
                      https://checkout.example/cart/example-item:1?source=partner
                  email: lin.example@reap.hk
                  shippingAddress:
                    firstName: Lin
                    lastName: Example
                    phone: '+85220000000'
                    addressLine1: 1 Fictional Road
                    city: Example
                    postalCode: '000001'
                    country: HK
                  offerCode: SAVE10
              oneOf:
                - type: object
                  properties:
                    email:
                      type: string
                      format: email
                      pattern: >-
                        ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
                    offerCode:
                      description: Offer code to apply while pricing the quote
                      type: string
                      minLength: 1
                      maxLength: 128
                      pattern: \S
                    items:
                      minItems: 1
                      maxItems: 20
                      type: array
                      items:
                        type: object
                        properties:
                          variantId:
                            type: string
                          quantity:
                            type: integer
                            exclusiveMinimum: 0
                            maximum: 9007199254740991
                        required:
                          - variantId
                          - quantity
                    shippingAddress:
                      type: object
                      properties:
                        firstName:
                          type: string
                          minLength: 1
                        lastName:
                          type: string
                          minLength: 1
                        phone:
                          type: string
                          pattern: ^\+[1-9]\d{6,14}$
                        addressLine1:
                          type: string
                          minLength: 1
                        addressLine2:
                          type: string
                        city:
                          type: string
                          minLength: 1
                        region:
                          type: string
                        postalCode:
                          type: string
                        country:
                          type: string
                      required:
                        - firstName
                        - lastName
                        - phone
                        - addressLine1
                        - city
                        - country
                  required:
                    - email
                    - items
                  additionalProperties: false
                  title: Reap discovery
                  not:
                    required:
                      - externalCheckout
                - type: object
                  properties:
                    email:
                      type: string
                      format: email
                      pattern: >-
                        ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
                    offerCode:
                      description: Offer code to apply while pricing the quote
                      type: string
                      minLength: 1
                      maxLength: 128
                      pattern: \S
                    externalCheckout:
                      type: object
                      properties:
                        merchantDomain:
                          type: string
                          minLength: 1
                          maxLength: 253
                        checkoutUrl:
                          type: string
                          maxLength: 8192
                          format: uri
                          description: >-
                            Caller-assembled checkout URL, preserved without
                            rebuilding. Its host must be `merchantDomain` or a
                            subdomain of it.
                      required:
                        - merchantDomain
                        - checkoutUrl
                      additionalProperties: false
                    shippingAddress:
                      type: object
                      properties:
                        firstName:
                          type: string
                          minLength: 1
                        lastName:
                          type: string
                          minLength: 1
                        phone:
                          type: string
                          pattern: ^\+[1-9]\d{6,14}$
                        addressLine1:
                          type: string
                          minLength: 1
                        addressLine2:
                          type: string
                        city:
                          type: string
                          minLength: 1
                        region:
                          type: string
                        postalCode:
                          type: string
                        country:
                          type: string
                      required:
                        - firstName
                        - lastName
                        - phone
                        - addressLine1
                        - city
                        - country
                  required:
                    - email
                    - externalCheckout
                    - shippingAddress
                  additionalProperties: false
                  title: Checkout URL
                  not:
                    required:
                      - items
          application/x-www-form-urlencoded:
            schema:
              examples:
                - items:
                    - variantId: var_123
                      quantity: 1
                  email: lin.example@reap.hk
                  offerCode: SAVE10
                - externalCheckout:
                    merchantDomain: checkout.example
                    checkoutUrl: >-
                      https://checkout.example/cart/example-item:1?source=partner
                  email: lin.example@reap.hk
                  shippingAddress:
                    firstName: Lin
                    lastName: Example
                    phone: '+85220000000'
                    addressLine1: 1 Fictional Road
                    city: Example
                    postalCode: '000001'
                    country: HK
                  offerCode: SAVE10
              oneOf:
                - type: object
                  properties:
                    email:
                      type: string
                      format: email
                      pattern: >-
                        ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
                    offerCode:
                      description: Offer code to apply while pricing the quote
                      type: string
                      minLength: 1
                      maxLength: 128
                      pattern: \S
                    items:
                      minItems: 1
                      maxItems: 20
                      type: array
                      items:
                        type: object
                        properties:
                          variantId:
                            type: string
                          quantity:
                            type: integer
                            exclusiveMinimum: 0
                            maximum: 9007199254740991
                        required:
                          - variantId
                          - quantity
                    shippingAddress:
                      type: object
                      properties:
                        firstName:
                          type: string
                          minLength: 1
                        lastName:
                          type: string
                          minLength: 1
                        phone:
                          type: string
                          pattern: ^\+[1-9]\d{6,14}$
                        addressLine1:
                          type: string
                          minLength: 1
                        addressLine2:
                          type: string
                        city:
                          type: string
                          minLength: 1
                        region:
                          type: string
                        postalCode:
                          type: string
                        country:
                          type: string
                      required:
                        - firstName
                        - lastName
                        - phone
                        - addressLine1
                        - city
                        - country
                  required:
                    - email
                    - items
                  additionalProperties: false
                  title: Reap discovery
                  not:
                    required:
                      - externalCheckout
                - type: object
                  properties:
                    email:
                      type: string
                      format: email
                      pattern: >-
                        ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
                    offerCode:
                      description: Offer code to apply while pricing the quote
                      type: string
                      minLength: 1
                      maxLength: 128
                      pattern: \S
                    externalCheckout:
                      type: object
                      properties:
                        merchantDomain:
                          type: string
                          minLength: 1
                          maxLength: 253
                        checkoutUrl:
                          type: string
                          maxLength: 8192
                          format: uri
                          description: >-
                            Caller-assembled checkout URL, preserved without
                            rebuilding. Its host must be `merchantDomain` or a
                            subdomain of it.
                      required:
                        - merchantDomain
                        - checkoutUrl
                      additionalProperties: false
                    shippingAddress:
                      type: object
                      properties:
                        firstName:
                          type: string
                          minLength: 1
                        lastName:
                          type: string
                          minLength: 1
                        phone:
                          type: string
                          pattern: ^\+[1-9]\d{6,14}$
                        addressLine1:
                          type: string
                          minLength: 1
                        addressLine2:
                          type: string
                        city:
                          type: string
                          minLength: 1
                        region:
                          type: string
                        postalCode:
                          type: string
                        country:
                          type: string
                      required:
                        - firstName
                        - lastName
                        - phone
                        - addressLine1
                        - city
                        - country
                  required:
                    - email
                    - externalCheckout
                    - shippingAddress
                  additionalProperties: false
                  title: Checkout URL
                  not:
                    required:
                      - items
          multipart/form-data:
            schema:
              examples:
                - items:
                    - variantId: var_123
                      quantity: 1
                  email: lin.example@reap.hk
                  offerCode: SAVE10
                - externalCheckout:
                    merchantDomain: checkout.example
                    checkoutUrl: >-
                      https://checkout.example/cart/example-item:1?source=partner
                  email: lin.example@reap.hk
                  shippingAddress:
                    firstName: Lin
                    lastName: Example
                    phone: '+85220000000'
                    addressLine1: 1 Fictional Road
                    city: Example
                    postalCode: '000001'
                    country: HK
                  offerCode: SAVE10
              oneOf:
                - type: object
                  properties:
                    email:
                      type: string
                      format: email
                      pattern: >-
                        ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
                    offerCode:
                      description: Offer code to apply while pricing the quote
                      type: string
                      minLength: 1
                      maxLength: 128
                      pattern: \S
                    items:
                      minItems: 1
                      maxItems: 20
                      type: array
                      items:
                        type: object
                        properties:
                          variantId:
                            type: string
                          quantity:
                            type: integer
                            exclusiveMinimum: 0
                            maximum: 9007199254740991
                        required:
                          - variantId
                          - quantity
                    shippingAddress:
                      type: object
                      properties:
                        firstName:
                          type: string
                          minLength: 1
                        lastName:
                          type: string
                          minLength: 1
                        phone:
                          type: string
                          pattern: ^\+[1-9]\d{6,14}$
                        addressLine1:
                          type: string
                          minLength: 1
                        addressLine2:
                          type: string
                        city:
                          type: string
                          minLength: 1
                        region:
                          type: string
                        postalCode:
                          type: string
                        country:
                          type: string
                      required:
                        - firstName
                        - lastName
                        - phone
                        - addressLine1
                        - city
                        - country
                  required:
                    - email
                    - items
                  additionalProperties: false
                  title: Reap discovery
                  not:
                    required:
                      - externalCheckout
                - type: object
                  properties:
                    email:
                      type: string
                      format: email
                      pattern: >-
                        ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
                    offerCode:
                      description: Offer code to apply while pricing the quote
                      type: string
                      minLength: 1
                      maxLength: 128
                      pattern: \S
                    externalCheckout:
                      type: object
                      properties:
                        merchantDomain:
                          type: string
                          minLength: 1
                          maxLength: 253
                        checkoutUrl:
                          type: string
                          maxLength: 8192
                          format: uri
                          description: >-
                            Caller-assembled checkout URL, preserved without
                            rebuilding. Its host must be `merchantDomain` or a
                            subdomain of it.
                      required:
                        - merchantDomain
                        - checkoutUrl
                      additionalProperties: false
                    shippingAddress:
                      type: object
                      properties:
                        firstName:
                          type: string
                          minLength: 1
                        lastName:
                          type: string
                          minLength: 1
                        phone:
                          type: string
                          pattern: ^\+[1-9]\d{6,14}$
                        addressLine1:
                          type: string
                          minLength: 1
                        addressLine2:
                          type: string
                        city:
                          type: string
                          minLength: 1
                        region:
                          type: string
                        postalCode:
                          type: string
                        country:
                          type: string
                      required:
                        - firstName
                        - lastName
                        - phone
                        - addressLine1
                        - city
                        - country
                  required:
                    - email
                    - externalCheckout
                    - shippingAddress
                  additionalProperties: false
                  title: Checkout URL
                  not:
                    required:
                      - items
      responses:
        '200':
          description: Response for status 200
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
                  shippingOptions:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: string
                        name:
                          type: string
                        selected:
                          type: boolean
                        price:
                          type: object
                          properties:
                            amount:
                              type: number
                            currency:
                              type: string
                              minLength: 3
                              maxLength: 3
                          required:
                            - amount
                            - currency
                        details:
                          type: array
                          items:
                            type: object
                            properties:
                              key:
                                type: string
                              value:
                                type: string
                            required:
                              - key
                              - value
                      required:
                        - id
                        - name
                        - selected
                        - price
                  amountBreakdown:
                    type: object
                    properties:
                      itemsSubtotal:
                        type: object
                        properties:
                          amount:
                            type: number
                          currency:
                            type: string
                            minLength: 3
                            maxLength: 3
                        required:
                          - amount
                          - currency
                      shipping:
                        type: object
                        properties:
                          amount:
                            type: number
                          currency:
                            type: string
                            minLength: 3
                            maxLength: 3
                        required:
                          - amount
                          - currency
                      tax:
                        type: object
                        properties:
                          amount:
                            type: object
                            properties:
                              amount:
                                type: number
                              currency:
                                type: string
                                minLength: 3
                                maxLength: 3
                            required:
                              - amount
                              - currency
                          includedInPrices:
                            type: boolean
                        required:
                          - amount
                      discounts:
                        type: array
                        items:
                          type: object
                          properties:
                            name:
                              type: string
                            amount:
                              type: object
                              properties:
                                amount:
                                  type: number
                                currency:
                                  type: string
                                  minLength: 3
                                  maxLength: 3
                              required:
                                - amount
                                - currency
                          required:
                            - name
                            - amount
                      additionalCharges:
                        type: array
                        items:
                          type: object
                          properties:
                            name:
                              type: string
                            amount:
                              type: object
                              properties:
                                amount:
                                  type: number
                                currency:
                                  type: string
                                  minLength: 3
                                  maxLength: 3
                              required:
                                - amount
                                - currency
                          required:
                            - name
                            - amount
                      finalAmount:
                        type: object
                        properties:
                          amount:
                            type: number
                          currency:
                            type: string
                            minLength: 3
                            maxLength: 3
                        required:
                          - amount
                          - currency
                    required:
                      - itemsSubtotal
                      - discounts
                      - additionalCharges
                      - finalAmount
                  expiresAt:
                    type: string
                    allOf:
                      - pattern: >-
                          ^(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))T(?:(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?(?:Z))$
                      - pattern: >-
                          ^(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))T(?:(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?(?:Z|([+-](?:[01]\d|2[0-3]):[0-5]\d)))$
                required:
                  - id
                  - shippingOptions
                  - amountBreakdown
                  - expiresAt
        '400':
          description: Response for status 400
          content:
            application/json:
              schema:
                anyOf:
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: AGENTIC_REQUEST_REJECTED
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: 'null'
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: AgenticRequestRejectedError
                    description: The request was rejected
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: CHECKOUT_URL_INVALID
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: object
                                properties:
                                  reason:
                                    type: string
                                    enum:
                                      - NOT_FOUND
                                      - EXPIRED
                                      - INVALID
                                      - MERCHANT_CONTEXT_UNVERIFIED
                                    example: NOT_FOUND
                                required:
                                  - reason
                                additionalProperties: false
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: CheckoutUrlInvalidError
                    description: The checkout URL cannot be used.
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: CARD_PAYMENT_UNAVAILABLE
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: 'null'
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: CardPaymentUnavailableError
                    description: Card payment is unavailable for this checkout.
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: OFFER_CODE_INVALID
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: 'null'
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: OfferCodeInvalidError
                    description: The offer code is invalid.
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: OFFER_CODE_EXPIRED
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: 'null'
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: OfferCodeExpiredError
                    description: The offer code has expired.
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: QUOTE_UNFULFILLABLE
                          message:
                            type: string
                          detail:
                            anyOf:
                              - oneOf:
                                  - type: object
                                    properties:
                                      reason:
                                        type: string
                                        const: INVALID_PHONE
                                      message:
                                        type: string
                                        const: Phone is invalid
                                    required:
                                      - reason
                                    additionalProperties: false
                                  - type: object
                                    properties:
                                      reason:
                                        type: string
                                        const: STATE_OR_PROVINCE_REQUIRED
                                      message:
                                        type: string
                                        const: Select a state / province
                                    required:
                                      - reason
                                    additionalProperties: false
                                  - type: object
                                    properties:
                                      reason:
                                        type: string
                                        const: ITEMS_UNSHIPPABLE
                                      message:
                                        type: string
                                        const: >-
                                          Your cart has been updated and the items
                                          you added can't be shipped to your
                                          address. Remove the items to complete
                                          your order.
                                    required:
                                      - reason
                                    additionalProperties: false
                                  - type: object
                                    properties:
                                      reason:
                                        type: string
                                        const: ADDRESS_LINE_2_REQUIRED
                                      message:
                                        type: string
                                        const: Address line 2 is required.
                                    required:
                                      - reason
                                    additionalProperties: false
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: QuoteUnfulfillableError
                    description: >-
                      The merchant cannot fulfill this quote as requested (for
                      example, it cannot ship to the address).
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: AGENTIC_REQUEST_REJECTED
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: object
                                properties:
                                  errors:
                                    minItems: 1
                                    maxItems: 20
                                    type: array
                                    items:
                                      type: object
                                      properties:
                                        field:
                                          type: string
                                          maxLength: 128
                                          pattern: ^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*$
                                          description: >-
                                            Dot-separated path to the rejected
                                            request field; array indices are numeric
                                            segments, such as `items.0.variantId`
                                        message:
                                          type: string
                                          enum:
                                            - Host must match the merchant domain
                                            - >-
                                              Must be a canonical DNS domain without a
                                              scheme, path, or www
                                            - >-
                                              Must be a valid checkout URL of at most
                                              8192 characters
                                            - Must be a valid checkout URL
                                            - Must use HTTPS
                                            - Must not include credentials
                                            - Must not include an explicit port
                                            - >-
                                              Must be a Shopify cart permalink or
                                              tokenized checkout continuation
                                            - >-
                                              shippingAddress is required for
                                              externalCheckout
                                            - >-
                                              Offer code must be at most 128
                                              characters
                                            - >-
                                              Exactly one of items or externalCheckout
                                              is required
                                          example: Host must match the merchant domain
                                      required:
                                        - field
                                      additionalProperties: false
                                required:
                                  - errors
                                additionalProperties: false
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: QuoteRequestRejectedError
                    description: The request was rejected
        '403':
          description: Agentic Payments is not enabled for this project
          content:
            application/json:
              schema:
                type: object
                properties:
                  error:
                    type: object
                    properties:
                      code:
                        type: string
                        const: AGENTIC_PAYMENTS_NOT_ENABLED
                      message:
                        type: string
                      detail:
                        anyOf:
                          - type: object
                            additionalProperties: {}
                          - type: 'null'
                    required:
                      - code
                      - message
                      - detail
                required:
                  - error
                title: AgenticPaymentsNotEnabledError
        '404':
          description: Resource not found
          content:
            application/json:
              schema:
                type: object
                properties:
                  error:
                    type: object
                    properties:
                      code:
                        type: string
                        const: AGENTIC_RESOURCE_NOT_FOUND
                      message:
                        type: string
                      detail:
                        anyOf:
                          - type: object
                            additionalProperties: {}
                          - type: 'null'
                    required:
                      - code
                      - message
                      - detail
                required:
                  - error
                title: AgenticResourceNotFoundError
        '409':
          description: Response for status 409
          content:
            application/json:
              schema:
                anyOf:
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: QUOTE_EXPIRED
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: 'null'
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: QuoteExpiredError
                    description: The quote has expired.
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: VARIANT_UNAVAILABLE
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: 'null'
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: VariantUnavailableError
                    description: >-
                      The selected item is sold out. Choose another variant or
                      product.
        '503':
          description: Response for status 503
          content:
            application/json:
              schema:
                anyOf:
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: AGENTIC_SERVICE_UNAVAILABLE
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: object
                                additionalProperties: {}
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: AgenticServiceUnavailableError
                    description: Agentic Payments is temporarily unavailable
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: QUOTE_TEMPORARILY_UNAVAILABLE
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: 'null'
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: QuoteTemporarilyUnavailableError
                    description: >-
                      We couldn't complete this quote right now. Please try
                      again in a moment.
          headers:
            Retry-After:
              description: Returned only when error.code is QUOTE_TEMPORARILY_UNAVAILABLE.
              schema:
                type: string
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      description: API key as Bearer token

````

This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.