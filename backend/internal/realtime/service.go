package realtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// PromptSource loads system prompts. store.SystemPromptRepo satisfies it.
type PromptSource interface {
	Get(ctx context.Context, key string) (domain.SystemPrompt, error)
}

// FallbackInstructions is used only when the DB has no "agent" prompt (fresh DB before seeding).
const FallbackInstructions = `You are Jarvis, the office procurement assistant for a Singapore office. Prices are in SGD.
Act only through your tools. Read back line items and totals before confirming. Always ask which
saved delivery address to use (call list_addresses) before calling confirm_order with that address_id.
Never say an order is placed until get_request_status shows "ordered". Never ask for card details.`

// VoiceAddendum is appended to the DB prompt for voice sessions.
const VoiceAddendum = `

## Voice session
You are speaking, not writing: keep replies short and natural. Spell out request ids only if asked.
Persona: a composed, courteous AI butler. Speak in a calm, measured, refined British manner with
light dry wit; address the user as "sir" or "ma'am" only if they ask you to.
Before calling confirm_order, ask which delivery address to use by label and wait for the answer.
After create_order_request, say in one short sentence that you are checking prices, then call
get_request_status once: it waits for the search. Never call it in a loop. Messages starting with
[Status update] come from the system: tell the user what changed, briefly, without being asked.`

// ServiceOptions configure Service.
type ServiceOptions struct {
	Minter  Minter
	Prompts PromptSource
	// Tools returns the tool definitions (normally agents.ToolExecutor.Definitions). Nil or an
	// empty result falls back to ToolDefinitions().
	Tools func() []llm.Tool
	Voice string // "" = minter default
	Model string // "" = minter default
	// DisableVoiceAddendum sends the DB prompt verbatim.
	DisableVoiceAddendum bool
}

// Service assembles a realtime session: DB system prompt (key "agent", loaded on every new
// session so admin edits apply immediately) + tool definitions, then mints a client secret.
type Service struct{ o ServiceOptions }

// NewService validates options.
func NewService(o ServiceOptions) (*Service, error) {
	if o.Minter == nil {
		return nil, fmt.Errorf("realtime: Minter is required: %w", domain.ErrValidation)
	}
	return &Service{o: o}, nil
}

// Instructions returns the prompt the session will use.
func (s *Service) Instructions(ctx context.Context) (string, error) {
	content := ""
	if s.o.Prompts != nil {
		p, err := s.o.Prompts.Get(ctx, domain.SystemPromptKeyAgent)
		switch {
		case err == nil:
			content = strings.TrimSpace(p.Content)
		case errors.Is(err, domain.ErrNotFound):
		default:
			return "", fmt.Errorf("realtime: load system prompt: %w", err)
		}
	}
	if content == "" {
		content = FallbackInstructions
	}
	if !s.o.DisableVoiceAddendum {
		content += VoiceAddendum
	}
	return content, nil
}

// Tools returns the session tool set.
func (s *Service) Tools() []llm.Tool {
	if s.o.Tools != nil {
		if t := s.o.Tools(); len(t) > 0 {
			return t
		}
	}
	return ToolDefinitions()
}

// Start mints a session for the user.
func (s *Service) Start(ctx context.Context, userID string) (Session, error) {
	instr, err := s.Instructions(ctx)
	if err != nil {
		return Session{}, err
	}
	tools := s.Tools()
	sess, err := s.o.Minter.Mint(ctx, SessionParams{
		Instructions: instr, Tools: tools, Voice: s.o.Voice, Model: s.o.Model, UserID: userID,
	})
	if err != nil {
		return Session{}, err
	}
	if sess.Tools == nil {
		sess.Tools = tools
	}
	return sess, nil
}
