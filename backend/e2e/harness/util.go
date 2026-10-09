// Package harness is the end-to-end test kit: wire-level fakes for Reap and OpenAI (httptest
// servers the real api binary talks to through its real HTTP clients), an SSE client, a JSON HTTP
// client, a launcher for the api binary and a Postgres database reset helper. Owner: agent J.
//
// Nothing here talks to the internet. API keys used with the fakes are random per run and are
// not secrets; real keys from backend/.env are never read.
package harness

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
)

// NewUUID returns a random RFC 4122 v4 UUID (Reap ids are UUIDs).
func NewUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// RandomKey returns a throwaway API key for the fakes ("e2e-" + 32 hex chars).
func RandomKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "e2e-" + hex.EncodeToString(b[:])
}

// round2 rounds a decimal SGD amount to cents.
func round2(f float64) float64 { return math.Round(f*100) / 100 }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func errf(format string, a ...any) error { return fmt.Errorf(format, a...) }
