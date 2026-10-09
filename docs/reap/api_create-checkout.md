> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Create checkout

> Opens the payment for an unexpired quote against an `ACTIVE` enrollment, whose stored card and owner are used for the charge. The response carries a redirect to a Reap-hosted approval page where the user reviews the order and approves the payment. After approval the user returns to `presentation.returnUrl` and Reap completes the merchant checkout.



## OpenAPI

````yaml /api-reference/openapi.json post /agentic/checkouts
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
  /agentic/checkouts:
    post:
      tags:
        - Agentic
      summary: Create checkout
      description: >-
        Opens the payment for an unexpired quote against an `ACTIVE` enrollment,
        whose stored card and owner are used for the charge. The response
        carries a redirect to a Reap-hosted approval page where the user reviews
        the order and approves the payment. After approval the user returns to
        `presentation.returnUrl` and Reap completes the merchant checkout.
      operationId: createCheckout_agentic
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
        - name: X-Simulate-Checkout
          in: header
          required: false
          schema:
            description: >-
              Simulates a completed checkout in the sandbox. This header is
              rejected in production.
            type: string
            const: COMPLETED
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                quoteId:
                  type: string
                  allOf:
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                enrollmentId:
                  type: string
                  allOf:
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                presentation:
                  type: object
                  properties:
                    type:
                      type: string
                      const: REDIRECT
                    returnUrl:
                      type: string
                      format: uri
                      description: HTTPS URL the browser returns to after the hosted flow
                  required:
                    - type
                    - returnUrl
              required:
                - quoteId
                - enrollmentId
                - presentation
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                quoteId:
                  type: string
                  allOf:
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                enrollmentId:
                  type: string
                  allOf:
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                presentation:
                  type: object
                  properties:
                    type:
                      type: string
                      const: REDIRECT
                    returnUrl:
                      type: string
                      format: uri
                      description: HTTPS URL the browser returns to after the hosted flow
                  required:
                    - type
                    - returnUrl
              required:
                - quoteId
                - enrollmentId
                - presentation
          multipart/form-data:
            schema:
              type: object
              properties:
                quoteId:
                  type: string
                  allOf:
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                enrollmentId:
                  type: string
                  allOf:
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                    - pattern: >-
                        ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                presentation:
                  type: object
                  properties:
                    type:
                      type: string
                      const: REDIRECT
                    returnUrl:
                      type: string
                      format: uri
                      description: HTTPS URL the browser returns to after the hosted flow
                  required:
                    - type
                    - returnUrl
              required:
                - quoteId
                - enrollmentId
                - presentation
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
                  status:
                    type: string
                    enum:
                      - REQUIRES_ACTION
                      - PROCESSING
                      - COMPLETED
                      - FAILED
                      - EXPIRED
                    example: REQUIRES_ACTION
                  quoteId:
                    type: string
                  enrollmentId:
                    anyOf:
                      - type: string
                      - type: 'null'
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
                  nextAction:
                    type: object
                    properties:
                      type:
                        type: string
                        enum:
                          - REDIRECT
                        example: REDIRECT
                      url:
                        type: string
                        format: uri
                      expiresAt:
                        type: string
                        allOf:
                          - pattern: >-
                              ^(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))T(?:(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?(?:Z))$
                          - pattern: >-
                              ^(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))T(?:(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?(?:Z|([+-](?:[01]\d|2[0-3]):[0-5]\d)))$
                    required:
                      - type
                      - url
                required:
                  - id
                  - status
                  - quoteId
                  - enrollmentId
                  - nextAction
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
                            const: AGENTIC_REQUEST_REJECTED
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: object
                                properties:
                                  field:
                                    type: string
                                    enum:
                                      - quoteId
                                      - enrollmentId
                                    example: quoteId
                                required:
                                  - field
                                additionalProperties: false
                              - type: 'null'
                        required:
                          - code
                          - message
                          - detail
                    required:
                      - error
                    title: CheckoutRequestRejectedError
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
                            const: ENROLLMENT_NOT_FOUND
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
                    title: EnrollmentNotFoundError
                    description: Enrollment not found.
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
                            const: ENROLLMENT_NOT_ACTIVE
                          message:
                            type: string
                          detail:
                            anyOf:
                              - type: object
                                properties:
                                  reason:
                                    type: string
                                    const: CARD_NOT_CAPTURED
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
                    title: EnrollmentNotActiveError
                    description: Enrollment is not active.
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
                            const: CHECKOUT_TEMPORARILY_UNAVAILABLE
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
                    title: CheckoutTemporarilyUnavailableError
                    description: >-
                      We couldn't complete this checkout right now. Please try
                      again in a moment.
          headers:
            Retry-After:
              description: >-
                Returned only when error.code is
                CHECKOUT_TEMPORARILY_UNAVAILABLE.
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