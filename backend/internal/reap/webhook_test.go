package reap

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

func TestVerifyWebhookSignature(t *testing.T) {
	const secret = "whsec_test_only"
	now := time.Unix(1709312400, 0)
	body := []byte(`{"id":"evt_1","type":"CHECKOUT_UPDATED","data":{"id":"c1","status":"COMPLETED"}}`)

	// Independent reference computation, exactly as the docs describe.
	ref := func(sec string, ts int64, b []byte) string {
		m := hmac.New(sha256.New, []byte(sec))
		m.Write([]byte(strconv.FormatInt(ts, 10) + "." + string(b)))
		return hex.EncodeToString(m.Sum(nil))
	}
	good := ref(secret, now.Unix(), body)
	hdr := func(ts int64, sig string) string { return "t=" + strconv.FormatInt(ts, 10) + ",v1=" + sig }

	tests := []struct {
		name   string
		header string
		body   []byte
		secret string
		now    time.Time
		want   error
	}{
		{"valid", hdr(now.Unix(), good), body, secret, now, nil},
		{"valid with spaces", "t=" + strconv.FormatInt(now.Unix(), 10) + ", v1=" + good, body, secret, now, nil},
		{"valid 4m59s old", hdr(now.Unix(), good), body, secret, now.Add(4*time.Minute + 59*time.Second), nil},
		{"valid clock skew future", hdr(now.Unix(), good), body, secret, now.Add(-4 * time.Minute), nil},
		{"rotation: second v1 matches", hdr(now.Unix(), ref("old", now.Unix(), body)) + ",v1=" + good, body, secret, now, nil},
		{"uppercase hex accepted", hdr(now.Unix(), toUpper(good)), body, secret, now, nil},
		{"stale", hdr(now.Unix(), good), body, secret, now.Add(5*time.Minute + time.Second), ErrWebhookStale},
		{"too far in future", hdr(now.Unix(), good), body, secret, now.Add(-6 * time.Minute), ErrWebhookStale},
		{"tampered body", hdr(now.Unix(), good), []byte(`{"id":"evt_1","type":"CHECKOUT_UPDATED","data":{"id":"c1","status":"FAILED"}}`), secret, now, ErrWebhookBadSignature},
		{"reserialised body", hdr(now.Unix(), good), []byte(`{"id": "evt_1", "type": "CHECKOUT_UPDATED", "data": {"id": "c1", "status": "COMPLETED"}}`), secret, now, ErrWebhookBadSignature},
		{"wrong secret", hdr(now.Unix(), ref("other", now.Unix(), body)), body, secret, now, ErrWebhookBadSignature},
		{"timestamp swapped", hdr(now.Unix()+1, good), body, secret, now, ErrWebhookBadSignature},
		{"non-hex sig", hdr(now.Unix(), "zz"), body, secret, now, ErrWebhookBadSignature},
		{"missing v1", "t=" + strconv.FormatInt(now.Unix(), 10), body, secret, now, ErrWebhookMalformedHeader},
		{"missing t", "v1=" + good, body, secret, now, ErrWebhookMalformedHeader},
		{"bad t", "t=abc,v1=" + good, body, secret, now, ErrWebhookMalformedHeader},
		{"empty header", "", body, secret, now, ErrWebhookMalformedHeader},
		{"no secret configured", hdr(now.Unix(), good), body, "", now, ErrWebhookNoSecret},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyWebhookSignature(tc.header, tc.body, tc.secret, tc.now)
			if !errors.Is(err, tc.want) && !(tc.want == nil && err == nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want != nil && tc.want != ErrWebhookNoSecret && !errors.Is(err, domain.ErrUnauthorized) {
				t.Errorf("verification failures must wrap ErrUnauthorized")
			}
		})
	}
}

func toUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func TestSignWebhookRoundTripAndParse(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"id":"evt_9","type":"CHECKOUT_UPDATED","data":{"id":"c9"}}`)
	h := SignWebhook("s3cret", now, body)
	ev, err := ParseWebhook(h, body, "s3cret", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != "evt_9" || ev.Type != "CHECKOUT_UPDATED" || string(ev.Data) != `{"id":"c9"}` {
		t.Fatalf("event %+v", ev)
	}
	if _, err := ParseWebhook(h, body, "nope", now); !errors.Is(err, ErrWebhookBadSignature) {
		t.Fatalf("err = %v", err)
	}
	bad := []byte(`not json`)
	if _, err := ParseWebhook(SignWebhook("s", now, bad), bad, "s", now); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v", err)
	}
	empty := []byte(`{"data":{}}`)
	if _, err := ParseWebhook(SignWebhook("s", now, empty), empty, "s", now); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v", err)
	}
}
