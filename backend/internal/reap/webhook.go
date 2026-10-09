package reap

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// WebhookSignatureHeader carries "t=<unix>,v1=<hex hmac-sha256>".
const WebhookSignatureHeader = "X-Reap-Webhook-Signature"

// WebhookIDHeader and WebhookTimestampHeader are informational delivery headers (docs/reap
// webhooks_overview.md). Only the signature header is authoritative.
const (
	WebhookIDHeader        = "X-Reap-Webhook-Id"
	WebhookTimestampHeader = "X-Reap-Webhook-Timestamp"
)

// WebhookTolerance is the max accepted |now - t| (docs: 5 minutes).
const WebhookTolerance = 5 * time.Minute

// WebhookEvent is the delivery envelope. Deliveries are at-least-once and unordered:
// dedupe on ID, ignore payloads whose data.updatedAt is older than what is stored.
type WebhookEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt *time.Time      `json:"createdAt,omitempty"`
	Data      json.RawMessage `json:"data"`
}

// Webhook verification failures. All wrap domain.ErrUnauthorized so the HTTP layer answers 4xx.
var (
	ErrWebhookMalformedHeader = fmt.Errorf("reap webhook: malformed signature header: %w", domain.ErrUnauthorized)
	ErrWebhookStale           = fmt.Errorf("reap webhook: timestamp outside tolerance: %w", domain.ErrUnauthorized)
	ErrWebhookBadSignature    = fmt.Errorf("reap webhook: signature mismatch: %w", domain.ErrUnauthorized)
	ErrWebhookNoSecret        = errors.New("reap webhook: signing secret not configured")
)

// VerifyWebhookSignature checks HMAC-SHA256 over "{t}.{rawBody}" with secret, in constant time,
// and rejects timestamps outside WebhookTolerance of now. Owner: agent C.
// Webhooks are optional (REAP_WEBHOOKS_ENABLED); polling GET /agentic/checkouts/:id is the default.
//
// rawBody must be the exact bytes received (never re-serialised JSON). Several v1= entries are
// accepted (any match passes) so signing-secret rotation does not break delivery.
func VerifyWebhookSignature(header string, rawBody []byte, secret string, now time.Time) error {
	if secret == "" {
		return ErrWebhookNoSecret
	}
	var (
		ts     string
		tsSet  bool
		hexSig []string
	)
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts, tsSet = v, true
		case "v1":
			hexSig = append(hexSig, v)
		}
	}
	if !tsSet || len(hexSig) == 0 {
		return ErrWebhookMalformedHeader
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || unix <= 0 {
		return ErrWebhookMalformedHeader
	}
	skew := now.Sub(time.Unix(unix, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > WebhookTolerance {
		return ErrWebhookStale
	}
	expected := computeSignature(secret, ts, rawBody)
	for _, s := range hexSig {
		got, err := hex.DecodeString(s)
		if err != nil {
			continue
		}
		if hmac.Equal(expected, got) {
			return nil
		}
	}
	return ErrWebhookBadSignature
}

// SignWebhook builds an X-Reap-Webhook-Signature header value for rawBody at time t. Used by the
// fake Reap server and by tests of webhook receivers; production code only verifies.
func SignWebhook(secret string, t time.Time, rawBody []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	return "t=" + ts + ",v1=" + hex.EncodeToString(computeSignature(secret, ts, rawBody))
}

func computeSignature(secret, ts string, rawBody []byte) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts))
	m.Write([]byte("."))
	m.Write(rawBody)
	return m.Sum(nil)
}

// ParseWebhook verifies the signature and then decodes the envelope.
func ParseWebhook(header string, rawBody []byte, secret string, now time.Time) (WebhookEvent, error) {
	var ev WebhookEvent
	if err := VerifyWebhookSignature(header, rawBody, secret, now); err != nil {
		return ev, err
	}
	if err := json.Unmarshal(rawBody, &ev); err != nil {
		return ev, fmt.Errorf("reap webhook: decode envelope: %v: %w", err, domain.ErrValidation)
	}
	if ev.ID == "" || ev.Type == "" {
		return ev, fmt.Errorf("reap webhook: envelope missing id/type: %w", domain.ErrValidation)
	}
	return ev, nil
}
