package realtime

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FakeCallsURL is returned by Fake; it is not routable, so a browser in FAKES mode should fall
// back to the text agent.
const FakeCallsURL = "https://realtime.fake.invalid/v1/realtime/calls"

// Fake is an offline Minter. It records every SessionParams and returns deterministic secrets.
type Fake struct {
	mu    sync.Mutex
	calls []SessionParams
	Err   error            // if set, Mint returns it
	Now   func() time.Time // default time.Now
	TTL   time.Duration    // default 1m
	Model string           // default DefaultModel
	Voice string           // default DefaultVoice
}

var _ Minter = (*Fake)(nil)

// NewFake returns a Fake with defaults.
func NewFake() *Fake { return &Fake{} }

// Mint implements Minter.
func (f *Fake) Mint(ctx context.Context, p SessionParams) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, p)
	n := len(f.calls)
	f.mu.Unlock()
	if f.Err != nil {
		return Session{}, f.Err
	}
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	ttl := f.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	return Session{
		ClientSecret: fmt.Sprintf("ek_fake_%d", n),
		ExpiresAt:    now().Add(ttl).UTC(),
		Model:        firstNonEmpty(p.Model, f.Model, DefaultModel),
		Voice:        firstNonEmpty(p.Voice, f.Voice, DefaultVoice),
		CallsURL:     FakeCallsURL,
		Tools:        p.Tools,
	}, nil
}

// Calls returns a copy of the recorded params.
func (f *Fake) Calls() []SessionParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SessionParams(nil), f.calls...)
}
