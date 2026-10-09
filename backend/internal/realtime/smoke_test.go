//go:build smoke

package realtime_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
)

// Mints a real ephemeral Realtime client secret. Run with:
// go test -tags smoke ./internal/realtime/ -run Smoke -v. The secret itself is never printed.
func TestSmoke_Mint(t *testing.T) {
	cfg, _ := config.Load("../../.env")
	if cfg.OpenAIAPIKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	m := realtime.NewOpenAIMinter(realtime.OpenAIOptions{
		APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIRealtimeModel, Voice: cfg.OpenAIRealtimeVoice,
		ExpiresAfter: time.Minute,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := m.Mint(ctx, realtime.SessionParams{Instructions: realtime.FallbackInstructions, Tools: realtime.ToolDefinitions()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.ClientSecret, "ek_") {
		t.Fatalf("unexpected secret format (len %d)", len(s.ClientSecret))
	}
	if time.Until(s.ExpiresAt) <= 0 || time.Until(s.ExpiresAt) > 2*time.Minute {
		t.Fatalf("expires_at = %s", s.ExpiresAt)
	}
	t.Logf("model=%s voice=%s calls_url=%s expires_in=%s tools=%d", s.Model, s.Voice, s.CallsURL, time.Until(s.ExpiresAt).Round(time.Second), len(s.Tools))
}
