package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// fakeStore implements only Audit(); every other method panics via the nil embedded interface.
type fakeStore struct {
	store.Store
	repo *fakeAuditRepo
}

func (f fakeStore) Audit() store.AuditRepo { return f.repo }

type fakeAuditRepo struct {
	store.AuditRepo
	got []domain.AuditEvent
	err error
}

func (r *fakeAuditRepo) Append(ctx context.Context, e *domain.AuditEvent) error {
	if r.err != nil {
		return r.err
	}
	e.ID = int64(len(r.got) + 1)
	r.got = append(r.got, *e)
	return nil
}

func TestStoreLoggerRecord(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
		actor     domain.ActorType
		typ       string
		payload   any
		wantErr   error
		wantJSON  string
		wantReqID bool
	}{
		{name: "struct payload", requestID: "r1", actor: domain.ActorUser, typ: RequestConfirmed,
			payload: map[string]string{"address_id": "a1"}, wantJSON: `{"address_id":"a1"}`, wantReqID: true},
		{name: "nil payload", actor: domain.ActorSystem, typ: AdminChanged, wantJSON: `{}`},
		{name: "raw payload", actor: domain.ActorAgent, typ: AgentToolCall,
			payload: json.RawMessage(`{"tool":"list_addresses"}`), wantJSON: `{"tool":"list_addresses"}`},
		{name: "scalar wrapped", actor: domain.ActorSystem, typ: "x.y", payload: 42, wantJSON: `{"value":42}`},
		{name: "card data scrubbed", actor: domain.ActorSystem, typ: EnrollmentStatus,
			payload: map[string]any{"status": "ACTIVE", "paymentMethod": map[string]any{"last4": "4242"},
				"nested": []any{map[string]any{"cvv": "123", "ok": true}}},
			wantJSON: `{"nested":[{"cvv":"[redacted]","ok":true}],"paymentMethod":"[redacted]","status":"ACTIVE"}`},
		{name: "missing type", actor: domain.ActorUser, typ: " ", wantErr: domain.ErrValidation},
		{name: "bad actor", actor: "robot", typ: "x", wantErr: domain.ErrValidation},
		{name: "invalid raw json", actor: domain.ActorUser, typ: "x", payload: json.RawMessage(`{`), wantErr: domain.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAuditRepo{}
			l := New(fakeStore{repo: repo})
			err := l.Record(context.Background(), tt.requestID, tt.actor, "u1", tt.typ, tt.payload)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if len(repo.got) != 0 {
					t.Fatal("event appended despite error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(repo.got) != 1 {
				t.Fatalf("appended %d events", len(repo.got))
			}
			e := repo.got[0]
			if string(e.Payload) != tt.wantJSON {
				t.Errorf("payload = %s, want %s", e.Payload, tt.wantJSON)
			}
			if (e.RequestID != nil) != tt.wantReqID {
				t.Errorf("request id set = %v", e.RequestID != nil)
			}
			if e.Type != tt.typ || e.ActorType != tt.actor || e.ActorID != "u1" {
				t.Errorf("event = %+v", e)
			}
		})
	}
}

func TestStoreLoggerPropagatesStoreError(t *testing.T) {
	repo := &fakeAuditRepo{err: errors.New("db down")}
	if err := New(fakeStore{repo: repo}).Record(context.Background(), "", domain.ActorSystem, "", "x", nil); err == nil {
		t.Fatal("want error")
	}
}

func TestIsSensitiveKey(t *testing.T) {
	for k, want := range map[string]bool{
		"last4": true, "LAST_4": true, "Last4": true, "card_number": true, "api-key": true,
		"Authorization": true, "client_secret": true, "status": false, "amount_cents": false, "email": false,
	} {
		if got := IsSensitiveKey(k); got != want {
			t.Errorf("IsSensitiveKey(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestMemoryLogger(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.Record(ctx, "r1", domain.ActorUser, "u", RequestCreated, nil)
	_ = m.Record(ctx, "r1", domain.ActorSystem, "", OrderCompleted, map[string]any{"secret": "x"})
	if got := strings.Join(m.Types(), ","); got != "request.created,order.completed" {
		t.Fatalf("types = %s", got)
	}
	if ev := m.Events(); string(ev[1].Payload) != `{"secret":"[redacted]"}` || ev[1].ID != 2 || ev[1].At.IsZero() {
		t.Fatalf("event = %+v", ev[1])
	}
	if err := (Nop{}).Record(ctx, "", domain.ActorUser, "", "x", nil); err != nil {
		t.Fatal(err)
	}
}
