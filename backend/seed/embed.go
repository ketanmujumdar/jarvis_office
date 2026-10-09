// Package seedfiles embeds the seed files that the running service needs at runtime (not only at
// seed time). Today that is the merchant allow-list, which is enforced on every vendor write and
// before every Reap quote.
package seedfiles

import _ "embed"

// AllowedMerchantsTSV is backend/seed/allowed_merchants.tsv (domain, category, country).
//
//go:embed allowed_merchants.tsv
var AllowedMerchantsTSV string
