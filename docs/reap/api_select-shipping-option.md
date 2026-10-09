> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Select shipping option

> Selects a shipping option on an existing quote and re-prices it. The response is the updated quote with a fresh amount breakdown. `QUOTE_REPLACEMENT_REQUIRED` means the caller must create a new quote.



## OpenAPI

````yaml /api-reference/openapi.json post /agentic/quotes/{id}/shipping-option
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
  /agentic/quotes/{id}/shipping-option:
    post:
      tags:
        - Agentic
      summary: Select shipping option
      description: >-
        Selects a shipping option on an existing quote and re-prices it. The
        response is the updated quote with a fresh amount breakdown.
        `QUOTE_REPLACEMENT_REQUIRED` means the caller must create a new quote.
      operationId: selectShippingOption_agentic
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
            minLength: 1
            description: Resource identifier
        - name: Reap-Version
          in: header
          required: true
          schema:
            type: string
            enum:
              - '2025-02-14'
            description: API version (YYYY-MM-DD)
            example: '2025-02-14'
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                shippingOptionId:
                  type: string
              required:
                - shippingOptionId
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                shippingOptionId:
                  type: string
              required:
                - shippingOptionId
          multipart/form-data:
            schema:
              type: object
              properties:
                shippingOptionId:
                  type: string
              required:
                - shippingOptionId
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
                            const: SHIPPING_OPTION_INVALID
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
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: ShippingOptionInvalidError
                    description: Shipping option is not available.
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
          description: Response for status 404
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
                    description: Resource not found
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: QUOTE_NOT_FOUND
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
                    title: QuoteNotFoundError
                    description: Quote not found.
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
                            const: QUOTE_REPLACEMENT_REQUIRED
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
                    title: QuoteReplacementRequiredError
                    description: Create a new quote.
                  - type: object
                    properties:
                      error:
                        type: object
                        properties:
                          code:
                            type: string
                            const: QUOTE_NOT_MUTABLE
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
                    title: QuoteNotMutableError
                    description: The quote can no longer be modified.
        '503':
          description: Agentic Payments is temporarily unavailable
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
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      description: API key as Bearer token

````

This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.