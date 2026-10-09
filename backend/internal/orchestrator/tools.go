package orchestrator

import (
	"context"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// Tools returns the orchestrator as an agents.OrderBackend for agents.NewTools.
func (o *Impl) Tools() agents.OrderBackend { return toolBackend{o} }

type toolBackend struct{ o *Impl }

func (b toolBackend) CreateOrder(ctx context.Context, userID string, args agents.CreateOrderRequestArgs) (domain.PurchaseRequest, error) {
	return b.o.CreateRequest(ctx, CreateRequestInput{RequesterID: userID, Utterance: args.Utterance, Items: args.Items})
}

func (b toolBackend) RequestDetail(ctx context.Context, id string) (domain.RequestDetail, error) {
	return b.o.GetRequest(ctx, id)
}

func (b toolBackend) ConfirmOrder(ctx context.Context, id, userID, addressID string) (domain.RequestDetail, error) {
	return b.o.Confirm(ctx, id, ConfirmInput{UserID: userID, AddressID: addressID})
}

func (b toolBackend) CancelOrder(ctx context.Context, id, userID string) (domain.RequestDetail, error) {
	return b.o.Cancel(ctx, id, userID)
}
