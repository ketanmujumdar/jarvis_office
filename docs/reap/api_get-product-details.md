> ## Documentation Index
> Fetch the complete documentation index at: https://docs.reap.global/llms.txt
> Use this file to discover all available pages before exploring further.

# Get product details

> Returns full details for a product, including its purchasable variants. When a product has options, use `POST /agentic/products/variant` to resolve a variant from the selected option ids. Quote line items reference the returned variant ids.



## OpenAPI

````yaml /api-reference/openapi.json post /agentic/products/details
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
  /agentic/products/details:
    post:
      tags:
        - Agentic
      summary: Get product details
      description: >-
        Returns full details for a product, including its purchasable variants.
        When a product has options, use `POST /agentic/products/variant` to
        resolve a variant from the selected option ids. Quote line items
        reference the returned variant ids.
      operationId: getProductDetails_agentic
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
                productIds:
                  minItems: 1
                  maxItems: 10
                  type: array
                  items:
                    type: string
              required:
                - productIds
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                productIds:
                  minItems: 1
                  maxItems: 10
                  type: array
                  items:
                    type: string
              required:
                - productIds
          multipart/form-data:
            schema:
              type: object
              properties:
                productIds:
                  minItems: 1
                  maxItems: 10
                  type: array
                  items:
                    type: string
              required:
                - productIds
      responses:
        '200':
          description: Response for status 200
          content:
            application/json:
              schema:
                type: object
                properties:
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
                        description:
                          type: string
                        media:
                          type: array
                          items:
                            type: object
                            properties:
                              type:
                                type: string
                              url:
                                type: string
                                format: uri
                              altText:
                                type: string
                            required:
                              - type
                              - url
                        options:
                          type: array
                          items:
                            type: object
                            properties:
                              name:
                                type: string
                              values:
                                type: array
                                items:
                                  type: object
                                  properties:
                                    optionId:
                                      type: string
                                    label:
                                      type: string
                                    available:
                                      type: boolean
                                  required:
                                    - optionId
                                    - label
                            required:
                              - name
                              - values
                        defaultVariant:
                          type: object
                          properties:
                            id:
                              type: string
                            name:
                              type: string
                            options:
                              type: array
                              items:
                                type: object
                                properties:
                                  name:
                                    type: string
                                  value:
                                    type: string
                                required:
                                  - name
                                  - value
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
                            requiresShipping:
                              type: boolean
                            media:
                              type: array
                              items:
                                type: object
                                properties:
                                  type:
                                    type: string
                                  url:
                                    type: string
                                    format: uri
                                  altText:
                                    type: string
                                required:
                                  - type
                                  - url
                          required:
                            - id
                            - options
                            - price
                            - media
                      required:
                        - id
                        - merchant
                        - name
                        - media
                        - options
                        - defaultVariant
                  errors:
                    type: array
                    items:
                      type: object
                      properties:
                        productId:
                          type: string
                        code:
                          type: string
                        message:
                          type: string
                      required:
                        - productId
                        - code
                required:
                  - products
                  - errors
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