> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Create enrollment

> Stores a card for later agentic purchases. `EXTERNAL` returns a redirect to a hosted card-entry page and becomes `ACTIVE` once the customer submits their card. `REAP_CARD` and `BIN_SPONSOR` are coming soon. Pass the enrollment id to `POST /agentic/checkouts` to charge it.



## OpenAPI

````yaml /api-reference/openapi.json post /agentic/enrollments
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
  /agentic/enrollments:
    post:
      tags:
        - Agentic
      summary: Create enrollment
      description: >-
        Stores a card for later agentic purchases. `EXTERNAL` returns a redirect
        to a hosted card-entry page and becomes `ACTIVE` once the customer
        submits their card. `REAP_CARD` and `BIN_SPONSOR` are coming soon. Pass
        the enrollment id to `POST /agentic/checkouts` to charge it.
      operationId: createEnrollment_agentic
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
        description: How the card is enrolled. The request shape depends on `source`.
        required: true
        content:
          application/json:
            schema:
              oneOf:
                - type: object
                  properties:
                    source:
                      type: string
                      const: REAP_CARD
                    cardId:
                      description: Card identifier to enroll
                      type: string
                      format: uuid
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: >-
                        Redirect config for the hosted Reap card device-binding
                        flow
                  required:
                    - source
                    - cardId
                    - presentation
                  description: Coming soon. Enroll an existing card.
                  title: Reap card
                - type: object
                  properties:
                    source:
                      type: string
                      const: BIN_SPONSOR
                    cardId:
                      type: string
                      minLength: 1
                      description: BIN-sponsor card identifier to enroll
                    owner:
                      $ref: '#/components/schemas/BinSponsorOwner'
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: >-
                        Redirect config for the hosted BIN-sponsor card
                        device-binding flow
                  required:
                    - source
                    - cardId
                    - owner
                    - presentation
                  description: Coming soon. Enroll a BIN-sponsor card.
                  title: BIN sponsor
                - type: object
                  properties:
                    source:
                      type: string
                      const: EXTERNAL
                    owner:
                      $ref: '#/components/schemas/ClientReferenceOwner'
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: Redirect config for the hosted card-entry flow
                  required:
                    - source
                    - owner
                    - presentation
                  description: Capture the card on a hosted page
                  title: External
          application/x-www-form-urlencoded:
            schema:
              oneOf:
                - type: object
                  properties:
                    source:
                      type: string
                      const: REAP_CARD
                    cardId:
                      description: Card identifier to enroll
                      type: string
                      format: uuid
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: >-
                        Redirect config for the hosted Reap card device-binding
                        flow
                  required:
                    - source
                    - cardId
                    - presentation
                  description: Coming soon. Enroll an existing card.
                  title: Reap card
                - type: object
                  properties:
                    source:
                      type: string
                      const: BIN_SPONSOR
                    cardId:
                      type: string
                      minLength: 1
                      description: BIN-sponsor card identifier to enroll
                    owner:
                      $ref: '#/components/schemas/BinSponsorOwner'
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: >-
                        Redirect config for the hosted BIN-sponsor card
                        device-binding flow
                  required:
                    - source
                    - cardId
                    - owner
                    - presentation
                  description: Coming soon. Enroll a BIN-sponsor card.
                  title: BIN sponsor
                - type: object
                  properties:
                    source:
                      type: string
                      const: EXTERNAL
                    owner:
                      $ref: '#/components/schemas/ClientReferenceOwner'
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: Redirect config for the hosted card-entry flow
                  required:
                    - source
                    - owner
                    - presentation
                  description: Capture the card on a hosted page
                  title: External
          multipart/form-data:
            schema:
              oneOf:
                - type: object
                  properties:
                    source:
                      type: string
                      const: REAP_CARD
                    cardId:
                      description: Card identifier to enroll
                      type: string
                      format: uuid
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: >-
                        Redirect config for the hosted Reap card device-binding
                        flow
                  required:
                    - source
                    - cardId
                    - presentation
                  description: Coming soon. Enroll an existing card.
                  title: Reap card
                - type: object
                  properties:
                    source:
                      type: string
                      const: BIN_SPONSOR
                    cardId:
                      type: string
                      minLength: 1
                      description: BIN-sponsor card identifier to enroll
                    owner:
                      $ref: '#/components/schemas/BinSponsorOwner'
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: >-
                        Redirect config for the hosted BIN-sponsor card
                        device-binding flow
                  required:
                    - source
                    - cardId
                    - owner
                    - presentation
                  description: Coming soon. Enroll a BIN-sponsor card.
                  title: BIN sponsor
                - type: object
                  properties:
                    source:
                      type: string
                      const: EXTERNAL
                    owner:
                      $ref: '#/components/schemas/ClientReferenceOwner'
                    presentation:
                      type: object
                      properties:
                        type:
                          type: string
                          const: REDIRECT
                        returnUrl:
                          type: string
                          format: uri
                          description: >-
                            HTTPS URL the browser returns to after the hosted
                            flow
                      required:
                        - type
                        - returnUrl
                      description: Redirect config for the hosted card-entry flow
                  required:
                    - source
                    - owner
                    - presentation
                  description: Capture the card on a hosted page
                  title: External
      responses:
        '200':
          description: Created enrollment. The response shape depends on `source`.
          content:
            application/json:
              schema:
                oneOf:
                  - $ref: '#/components/schemas/ReapCardEnrollmentCreateResponse'
                    title: ReapCard
                  - $ref: '#/components/schemas/BinSponsorEnrollmentCreateResponse'
                    title: BinSponsor
                  - $ref: '#/components/schemas/ExternalEnrollmentCreateResponse'
                    title: External
                title: Enrollment create
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
                            const: AGENTIC_CARD_NOT_FOUND
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
                    title: AgenticCardNotFoundError
                    description: Card not found
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
    BinSponsorOwner:
      type: object
      properties:
        type:
          type: string
          const: CLIENT_REFERENCE
        id:
          type: string
          minLength: 1
          description: The client customer identifier
        email:
          type: string
          format: email
          pattern: >-
            ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
          description: Customer email address
        name:
          type: string
          minLength: 1
          maxLength: 255
          description: Cardholder name
      required:
        - type
        - id
        - email
        - name
      description: The client customer that owns this BIN-sponsor enrollment
      title: Client reference
    ClientReferenceOwner:
      type: object
      properties:
        type:
          type: string
          const: CLIENT_REFERENCE
        id:
          type: string
          minLength: 1
          description: The client customer identifier
        email:
          type: string
          format: email
          pattern: >-
            ^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$
          description: Customer email address
      required:
        - type
        - id
        - email
      description: The client customer that owns this enrollment
      title: Client reference
    ReapCardEnrollmentCreateResponse:
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
        source:
          type: string
          const: REAP_CARD
        owner:
          $ref: '#/components/schemas/ReapUserOwner'
          description: Cardholder that owns this enrollment
      required:
        - id
        - status
        - nextAction
        - source
        - owner
      description: Coming soon. Enrollment created from an existing card.
      title: Reap card
    BinSponsorEnrollmentCreateResponse:
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
        source:
          type: string
          const: BIN_SPONSOR
        owner:
          $ref: '#/components/schemas/BinSponsorOwner'
      required:
        - id
        - status
        - nextAction
        - source
        - owner
      description: Coming soon. Enrollment created from a BIN-sponsor card.
      title: BIN sponsor
    ExternalEnrollmentCreateResponse:
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
        source:
          type: string
          const: EXTERNAL
        owner:
          $ref: '#/components/schemas/ClientReferenceOwner'
      required:
        - id
        - status
        - nextAction
        - source
        - owner
      description: Enrollment that needs a hosted card-entry redirect
      title: External
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
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      description: API key as Bearer token

````

This documentation is built and hosted on [Mintlify](https://mintlify.com), a developer documentation platform.