> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Search products

> Searches merchant catalogs for products matching a free-text query, with optional merchant preference and result filters. Use `POST /agentic/products/details` on a result to resolve its purchasable variants.



## OpenAPI

````yaml /api-reference/openapi.json post /agentic/products/search
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
  /agentic/products/search:
    post:
      tags:
        - Agentic
      summary: Search products
      description: >-
        Searches merchant catalogs for products matching a free-text query, with
        optional merchant preference and result filters. Use `POST
        /agentic/products/details` on a result to resolve its purchasable
        variants.
      operationId: searchProducts_agentic
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
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                query:
                  type: string
                  minLength: 1
                merchantPreference:
                  type: object
                  properties:
                    mode:
                      type: string
                      enum:
                        - PREFER
                        - ONLY
                      example: PREFER
                    merchantName:
                      type: string
                      minLength: 1
                  required:
                    - mode
                    - merchantName
                context:
                  type: object
                  properties:
                    country:
                      type: string
                    currency:
                      type: string
                filters:
                  type: object
                  properties:
                    price:
                      type: object
                      properties:
                        min:
                          type: string
                        max:
                          type: string
                    availability:
                      type: string
                      enum:
                        - AVAILABLE_ONLY
                      example: AVAILABLE_ONLY
                pagination:
                  type: object
                  properties:
                    cursor:
                      anyOf:
                        - type: string
                        - type: 'null'
                    limit:
                      type: integer
                      minimum: 1
                      maximum: 50
              required:
                - query
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                query:
                  type: string
                  minLength: 1
                merchantPreference:
                  type: object
                  properties:
                    mode:
                      type: string
                      enum:
                        - PREFER
                        - ONLY
                      example: PREFER
                    merchantName:
                      type: string
                      minLength: 1
                  required:
                    - mode
                    - merchantName
                context:
                  type: object
                  properties:
                    country:
                      type: string
                    currency:
                      type: string
                filters:
                  type: object
                  properties:
                    price:
                      type: object
                      properties:
                        min:
                          type: string
                        max:
                          type: string
                    availability:
                      type: string
                      enum:
                        - AVAILABLE_ONLY
                      example: AVAILABLE_ONLY
                pagination:
                  type: object
                  properties:
                    cursor:
                      anyOf:
                        - type: string
                        - type: 'null'
                    limit:
                      type: integer
                      minimum: 1
                      maximum: 50
              required:
                - query
          multipart/form-data:
            schema:
              type: object
              properties:
                query:
                  type: string
                  minLength: 1
                merchantPreference:
                  type: object
                  properties:
                    mode:
                      type: string
                      enum:
                        - PREFER
                        - ONLY
                      example: PREFER
                    merchantName:
                      type: string
                      minLength: 1
                  required:
                    - mode
                    - merchantName
                context:
                  type: object
                  properties:
                    country:
                      type: string
                    currency:
                      type: string
                filters:
                  type: object
                  properties:
                    price:
                      type: object
                      properties:
                        min:
                          type: string
                        max:
                          type: string
                    availability:
                      type: string
                      enum:
                        - AVAILABLE_ONLY
                      example: AVAILABLE_ONLY
                pagination:
                  type: object
                  properties:
                    cursor:
                      anyOf:
                        - type: string
                        - type: 'null'
                    limit:
                      type: integer
                      minimum: 1
                      maximum: 50
              required:
                - query
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
                  products:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: string
                        merchant:
                          type: object
                          properties:
                            name:
                              type: string
                          required:
                            - name
                        name:
                          type: string
                        imageUrl:
                          type: string
                          format: uri
                        priceRange:
                          type: object
                          properties:
                            min:
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
                            max:
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
                            - min
                            - max
                        available:
                          type: boolean
                        previewVariant:
                          type: object
                          properties:
                            id:
                              type: string
                            name:
                              type: string
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
                            available:
                              type: boolean
                          required:
                            - id
                            - price
                      required:
                        - id
                        - merchant
                        - name
                        - priceRange
                  pagination:
                    type: object
                    properties:
                      nextCursor:
                        anyOf:
                          - type: string
                          - type: 'null'
                      hasNextPage:
                        type: boolean
                      returnedCount:
                        type: integer
                        minimum: -9007199254740991
                        maximum: 9007199254740991
                    required:
                      - nextCursor
                      - hasNextPage
                      - returnedCount
                  warnings:
                    type: array
                    items:
                      type: string
                required:
                  - id
                  - products
                  - pagination
                  - warnings
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
                            const: MERCHANT_NOT_RESOLVED
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
                    title: MerchantNotResolvedError
                    description: >-
                      Merchant preference could not be resolved. Correct the
                      merchant name or remove the restriction.
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