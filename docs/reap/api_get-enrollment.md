> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Get enrollment

> Agentic Payments - enrollments, mandates, products, quotes, and checkouts.



## OpenAPI

````yaml /api-reference/openapi.json get /agentic/enrollments/{id}
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
  /agentic/enrollments/{id}:
    get:
      tags:
        - Agentic
      summary: Get enrollment
      description: >-
        Agentic Payments - enrollments, mandates, products, quotes, and
        checkouts.
      operationId: getEnrollment_agentic
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
                    allOf:
                      - pattern: >-
                          ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                      - pattern: >-
                          ^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$
                  status:
                    type: string
                    enum:
                      - REQUIRES_ACTION
                      - ACTIVE
                      - FAILED
                      - EXPIRED
                      - REVOKED
                    example: REQUIRES_ACTION
                  owner:
                    oneOf:
                      - $ref: '#/components/schemas/ReapUserOwner'
                        title: ReapUser
                      - $ref: '#/components/schemas/EnrollmentClientReferenceOwner'
                  paymentMethod:
                    anyOf:
                      - type: object
                        properties:
                          type:
                            type: string
                            enum:
                              - CARD
                            example: CARD
                          network:
                            type: string
                          last4:
                            type: string
                            minLength: 4
                            maxLength: 4
                          expiryMonth:
                            type: integer
                            minimum: 1
                            maximum: 12
                          expiryYear:
                            type: integer
                            minimum: -9007199254740991
                            maximum: 9007199254740991
                        required:
                          - type
                          - network
                          - last4
                          - expiryMonth
                          - expiryYear
                        title: Card
                      - type: 'null'
                  nextAction:
                    anyOf:
                      - type: object
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
                        title: Redirect
                      - type: 'null'
                  createdAt:
                    type: string
                    allOf:
                      - pattern: >-
                          ^(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))T(?:(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?(?:Z))$
                      - pattern: >-
                          ^(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))T(?:(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?(?:Z))$
                  updatedAt:
                    type: string
                    allOf:
                      - pattern: >-
                          ^(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))T(?:(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?(?:Z))$
                      - pattern: >-
                          ^(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))T(?:(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?(?:Z))$
                required:
                  - id
                  - status
                  - owner
                  - paymentMethod
                  - nextAction
                  - createdAt
                  - updatedAt
        '400':
          description: The request was rejected
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
  schemas:
    ReapUserOwner:
      type: object
      properties:
        type:
          type: string
          const: REAP_USER
        id:
          description: User identifier
          type: string
          format: uuid
      required:
        - type
        - id
      title: Reap user
    EnrollmentClientReferenceOwner:
      type: object
      properties:
        type:
          type: string
          const: CLIENT_REFERENCE
        id:
          type: string
          minLength: 1
        email:
          type: string
          maxLength: 320
          allOf:
            - pattern: >-
                ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
            - pattern: >-
                ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
        name:
          type: string
      required:
        - type
        - id
      description: Client customer that owns the enrollment
      title: Client reference
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      description: API key as Bearer token

````

This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.