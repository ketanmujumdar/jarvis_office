//go:build smoke

package llm_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// Real OpenAI calls. Run with: go test -tags smoke ./internal/llm/ -run Smoke -v
// Keys come from backend/.env; nothing secret is printed.
func smokeClient(t *testing.T) *llm.OpenAI {
	t.Helper()
	cfg, _ := config.Load("../../.env")
	if cfg.OpenAIAPIKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	return llm.NewOpenAI(llm.OpenAIOptions{APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIModel, Timeout: cfg.LLMTimeout})
}

func TestSmoke_Chat(t *testing.T) {
	c := smokeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := c.Chat(ctx, llm.ChatRequest{
		Messages:        []llm.Message{{Role: llm.RoleUser, Content: "Reply with exactly the word: pong"}},
		MaxOutputTokens: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model=%s finish=%s usage=%+v latency=%s reply=%q", resp.Model, resp.FinishReason, resp.Usage, resp.Latency, resp.Message.Content)
	if !strings.Contains(strings.ToLower(resp.Message.Content), "pong") {
		t.Fatalf("reply = %q", resp.Message.Content)
	}
}

func TestSmoke_ToolCall(t *testing.T) {
	c := smokeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := c.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "You are an office procurement assistant. Use tools."},
			{Role: llm.RoleUser, Content: "Please order 10 reams of A4 paper."},
		},
		Tools: []llm.Tool{{Name: "create_order_request", Description: "Start a purchase request.",
			Parameters: []byte(`{"type":"object","properties":{"utterance":{"type":"string"}},"required":["utterance"]}`)}},
		ToolChoice: llm.ToolChoiceRequired,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Name != "create_order_request" {
		t.Fatalf("message = %+v", resp.Message)
	}
	t.Logf("args=%s", resp.Message.ToolCalls[0].Arguments)
}

func TestSmoke_ExtractAndMatch(t *testing.T) {
	c := smokeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	items, err := llm.ExtractLineItems(ctx, c, llm.ExtractInput{
		Utterance: "We need 5 reams of printer paper and the usual pens, asap",
		Catalog:   hints,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("items=%s", mustJSON(items))
	if len(items) != 2 || items[0].CatalogSKU != "a4-copier-paper-ream" || items[0].Qty == nil || *items[0].Qty != 5 {
		t.Fatalf("items = %s", mustJSON(items))
	}
	m, err := llm.MatchOffers(ctx, c, llm.MatchTarget{Name: "Ballpoint Pen 0.7mm", Unit: "pen", MaxUnitPriceCents: 250},
		[]llm.OfferCandidate{{Title: "PILOT Rexgrip Ballpoint Pen 0.7mm Blue, Pack of 12", PriceCents: 1500}, {Title: "Ceramic coffee mug", PriceCents: 900}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("matches=%s", mustJSON(m))
	if !m[0].IsMatch || m[0].PackSize != 12 || m[0].UnitPriceCents != 125 || m[1].IsMatch {
		t.Fatalf("matches = %s", mustJSON(m))
	}
}
