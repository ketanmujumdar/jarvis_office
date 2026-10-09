package reap

import "time"

// Types mirror docs/reap/api_*.md (Reap-Version 2025-02-14) field-for-field. Amounts are decimal
// numbers in major units (e.g. 35.5 = S$35.50); convert with domain.CentsFromFloat.

// Money is Reap's {amount, currency}.
type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// Merchant is the merchant block on products. Reap returns only the name.
type Merchant struct {
	Name string `json:"name"`
}

// NextAction is a redirect to a Reap-hosted page (card entry or checkout approval).
type NextAction struct {
	Type      string     `json:"type"` // "REDIRECT"
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// Presentation tells Reap where to send the browser after a hosted page.
type Presentation struct {
	Type      string `json:"type"`      // always "REDIRECT"
	ReturnURL string `json:"returnUrl"` // HTTPS
}

// ---------- POST /agentic/products/search ----------

// MerchantPreferenceMode for search.
type MerchantPreferenceMode string

const (
	MerchantPrefer MerchantPreferenceMode = "PREFER"
	MerchantOnly   MerchantPreferenceMode = "ONLY"
)

// MerchantPreference restricts or biases search to one merchant. Probe finding: MerchantName must be
// the merchant DOMAIN (e.g. "popular.com.sg"); display names often fail with MERCHANT_NOT_RESOLVED.
type MerchantPreference struct {
	Mode         MerchantPreferenceMode `json:"mode"`
	MerchantName string                 `json:"merchantName"`
}

type SearchContext struct {
	Country  string `json:"country,omitempty"`
	Currency string `json:"currency,omitempty"`
}

type PriceFilter struct {
	Min string `json:"min,omitempty"` // decimal string
	Max string `json:"max,omitempty"`
}

type SearchFilters struct {
	Price        *PriceFilter `json:"price,omitempty"`
	Availability string       `json:"availability,omitempty"` // "AVAILABLE_ONLY"
}

type Pagination struct {
	Cursor *string `json:"cursor,omitempty"`
	Limit  int     `json:"limit,omitempty"` // 1..50
}

type SearchRequest struct {
	Query              string              `json:"query"`
	MerchantPreference *MerchantPreference `json:"merchantPreference,omitempty"`
	Context            *SearchContext      `json:"context,omitempty"`
	Filters            *SearchFilters      `json:"filters,omitempty"`
	Pagination         *Pagination         `json:"pagination,omitempty"`
}

type PriceRange struct {
	Min Money `json:"min"`
	Max Money `json:"max"`
}

type PreviewVariant struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Price     Money  `json:"price"`
	Available *bool  `json:"available,omitempty"`
}

type SearchProduct struct {
	ID             string          `json:"id"`
	Merchant       Merchant        `json:"merchant"`
	Name           string          `json:"name"`
	ImageURL       string          `json:"imageUrl,omitempty"`
	PriceRange     PriceRange      `json:"priceRange"`
	Available      *bool           `json:"available,omitempty"`
	PreviewVariant *PreviewVariant `json:"previewVariant,omitempty"`
}

type PageInfo struct {
	NextCursor    *string `json:"nextCursor"`
	HasNextPage   bool    `json:"hasNextPage"`
	ReturnedCount int     `json:"returnedCount"`
}

type SearchResponse struct {
	ID         string          `json:"id"`
	Products   []SearchProduct `json:"products"`
	Pagination PageInfo        `json:"pagination"`
	Warnings   []string        `json:"warnings"`
}

// ---------- POST /agentic/products/details ----------

type DetailsRequest struct {
	ProductIDs []string `json:"productIds"` // 1..10
}

type Media struct {
	Type    string `json:"type"`
	URL     string `json:"url"`
	AltText string `json:"altText,omitempty"`
}

type OptionValue struct {
	OptionID  string `json:"optionId"`
	Label     string `json:"label"`
	Available *bool  `json:"available,omitempty"`
}

type ProductOption struct {
	Name   string        `json:"name"`
	Values []OptionValue `json:"values"`
}

type SelectedOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Variant is a purchasable variant (defaultVariant in details; response of /products/variant).
type Variant struct {
	ID               string           `json:"id"`
	Name             string           `json:"name,omitempty"`
	Options          []SelectedOption `json:"options"`
	Price            Money            `json:"price"`
	Available        *bool            `json:"available,omitempty"`
	RequiresShipping *bool            `json:"requiresShipping,omitempty"`
	Media            []Media          `json:"media,omitempty"`
}

type ProductDetails struct {
	ID             string          `json:"id"`
	Merchant       Merchant        `json:"merchant"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	Media          []Media         `json:"media"`
	Options        []ProductOption `json:"options"`
	DefaultVariant Variant         `json:"defaultVariant"`
}

type DetailsError struct {
	ProductID string `json:"productId"`
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
}

type DetailsResponse struct {
	Products []ProductDetails `json:"products"`
	Errors   []DetailsError   `json:"errors"`
}

// ---------- POST /agentic/products/variant ----------

type VariantRequest struct {
	ProductID string   `json:"productId"`
	OptionIDs []string `json:"optionIds"`
}

// ---------- POST /agentic/quotes, GET /agentic/quotes/:id, POST /agentic/quotes/:id/shipping-option ----------

type QuoteItem struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
}

// ShippingAddress for quotes. Required when any item requires shipping.
type ShippingAddress struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	Phone        string `json:"phone"` // ^\+[1-9]\d{6,14}$
	AddressLine1 string `json:"addressLine1"`
	AddressLine2 string `json:"addressLine2,omitempty"`
	City         string `json:"city"`
	Region       string `json:"region,omitempty"`
	PostalCode   string `json:"postalCode,omitempty"`
	Country      string `json:"country"`
}

type ExternalCheckout struct {
	MerchantDomain string `json:"merchantDomain"`
	CheckoutURL    string `json:"checkoutUrl"`
}

// CreateQuoteRequest: send exactly one of Items or ExternalCheckout. We use Items.
type CreateQuoteRequest struct {
	Email            string            `json:"email"`
	Items            []QuoteItem       `json:"items,omitempty"` // 1..20, all from one merchant
	ExternalCheckout *ExternalCheckout `json:"externalCheckout,omitempty"`
	ShippingAddress  *ShippingAddress  `json:"shippingAddress,omitempty"`
	OfferCode        string            `json:"offerCode,omitempty"`
}

type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ShippingOption struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Selected bool       `json:"selected"`
	Price    Money      `json:"price"`
	Details  []KeyValue `json:"details,omitempty"`
}

type Tax struct {
	Amount           Money `json:"amount"`
	IncludedInPrices bool  `json:"includedInPrices"`
}

type NamedAmount struct {
	Name   string `json:"name"`
	Amount Money  `json:"amount"`
}

type AmountBreakdown struct {
	ItemsSubtotal     Money         `json:"itemsSubtotal"`
	Shipping          *Money        `json:"shipping,omitempty"`
	Tax               *Tax          `json:"tax,omitempty"`
	Discounts         []NamedAmount `json:"discounts"`
	AdditionalCharges []NamedAmount `json:"additionalCharges"`
	FinalAmount       Money         `json:"finalAmount"`
}

type Quote struct {
	ID              string           `json:"id"`
	ShippingOptions []ShippingOption `json:"shippingOptions"`
	AmountBreakdown AmountBreakdown  `json:"amountBreakdown"`
	ExpiresAt       time.Time        `json:"expiresAt"`
}

type SelectShippingRequest struct {
	ShippingOptionID string `json:"shippingOptionId"`
}

// ---------- POST /agentic/checkouts, GET /agentic/checkouts/:id ----------

type CheckoutStatus string

const (
	CheckoutRequiresAction CheckoutStatus = "REQUIRES_ACTION"
	CheckoutProcessing     CheckoutStatus = "PROCESSING"
	CheckoutCompleted      CheckoutStatus = "COMPLETED"
	CheckoutFailed         CheckoutStatus = "FAILED"
	CheckoutExpired        CheckoutStatus = "EXPIRED"
)

// Terminal reports whether polling can stop.
func (s CheckoutStatus) Terminal() bool {
	return s == CheckoutCompleted || s == CheckoutFailed || s == CheckoutExpired
}

type CreateCheckoutRequest struct {
	QuoteID      string       `json:"quoteId"`
	EnrollmentID string       `json:"enrollmentId"`
	Presentation Presentation `json:"presentation"`
}

// Checkout is the union of the create and get responses. On create, Amount is set; on get,
// FinalAmount and OrderID (once COMPLETED). NextAction nil means no user action is needed.
type Checkout struct {
	ID           string         `json:"id"`
	Status       CheckoutStatus `json:"status"`
	QuoteID      string         `json:"quoteId"`
	EnrollmentID string         `json:"enrollmentId,omitempty"`
	Amount       *Money         `json:"amount,omitempty"`
	FinalAmount  *Money         `json:"finalAmount,omitempty"`
	OrderID      *string        `json:"orderId,omitempty"`
	NextAction   *NextAction    `json:"nextAction"`
	CreatedAt    *time.Time     `json:"createdAt,omitempty"`
	UpdatedAt    *time.Time     `json:"updatedAt,omitempty"`
}

// ---------- POST /agentic/enrollments, GET /agentic/enrollments/:id ----------

type EnrollmentStatus string

const (
	EnrollmentRequiresAction EnrollmentStatus = "REQUIRES_ACTION"
	EnrollmentActive         EnrollmentStatus = "ACTIVE"
	EnrollmentFailed         EnrollmentStatus = "FAILED"
	EnrollmentExpired        EnrollmentStatus = "EXPIRED"
	EnrollmentRevoked        EnrollmentStatus = "REVOKED"
)

// Owner of an enrollment. For EXTERNAL we send {type: CLIENT_REFERENCE, id, email}.
type Owner struct {
	Type  string `json:"type"` // "CLIENT_REFERENCE" | "REAP_USER"
	ID    string `json:"id"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

// CreateEnrollmentRequest for source EXTERNAL (REAP_CARD and BIN_SPONSOR are "coming soon").
type CreateEnrollmentRequest struct {
	Source       string       `json:"source"` // "EXTERNAL"
	Owner        Owner        `json:"owner"`
	Presentation Presentation `json:"presentation"`
}

// PaymentMethod is returned by GET enrollment. We read it but NEVER persist it (no card data).
type PaymentMethod struct {
	Type        string `json:"type"`
	Network     string `json:"network"`
	Last4       string `json:"last4"`
	ExpiryMonth int    `json:"expiryMonth"`
	ExpiryYear  int    `json:"expiryYear"`
}

type Enrollment struct {
	ID            string           `json:"id"`
	Status        EnrollmentStatus `json:"status"`
	Source        string           `json:"source,omitempty"`
	Owner         Owner            `json:"owner"`
	PaymentMethod *PaymentMethod   `json:"paymentMethod,omitempty"`
	NextAction    *NextAction      `json:"nextAction"`
	CreatedAt     *time.Time       `json:"createdAt,omitempty"`
	UpdatedAt     *time.Time       `json:"updatedAt,omitempty"`
}
