package domain

import "fmt"

// RequestStatus is the PurchaseRequest state.
//
//	parsing ──> searching ──> quoted ──confirm(address)──┬─(all AUTO_APPROVE)──> approved
//	                                                     └─(any NEEDS_APPROVAL)─> pending_approval ─approve─> approved
//	                                                                                             └─reject──> rejected
//	approved ──> checking_out (Reap quotes + drift check) ─┬─ drift > pct ──> pending_approval (kind price_drift)
//	                                                       └─ ok ──> awaiting_payment (checkout nextAction URL shown)
//	awaiting_payment ──> paying (Reap PROCESSING) ──> ordered (all checkouts COMPLETED)
//
// Any non-terminal state may go to failed (with failure_reason) or cancelled (by the user, only
// before money can move: not once a Reap checkout exists, i.e. not from awaiting_payment or
// paying; Reap has no checkout-cancel API, so an open approval link could still be paid). REJECT lines are dropped from the basket at
// quote time; if every line is REJECT the request goes quoted -> rejected.
type RequestStatus string

const (
	StatusParsing         RequestStatus = "parsing"
	StatusSearching       RequestStatus = "searching"
	StatusQuoted          RequestStatus = "quoted"
	StatusPendingApproval RequestStatus = "pending_approval"
	StatusApproved        RequestStatus = "approved"
	StatusCheckingOut     RequestStatus = "checking_out"
	StatusAwaitingPayment RequestStatus = "awaiting_payment"
	StatusPaying          RequestStatus = "paying"
	StatusOrdered         RequestStatus = "ordered"
	StatusRejected        RequestStatus = "rejected"
	StatusFailed          RequestStatus = "failed"
	StatusCancelled       RequestStatus = "cancelled"
)

// AllStatuses in flow order.
var AllStatuses = []RequestStatus{
	StatusParsing, StatusSearching, StatusQuoted, StatusPendingApproval, StatusApproved,
	StatusCheckingOut, StatusAwaitingPayment, StatusPaying, StatusOrdered,
	StatusRejected, StatusFailed, StatusCancelled,
}

var transitions = map[RequestStatus][]RequestStatus{
	StatusParsing:         {StatusSearching, StatusFailed, StatusCancelled},
	StatusSearching:       {StatusQuoted, StatusFailed, StatusCancelled},
	StatusQuoted:          {StatusApproved, StatusPendingApproval, StatusRejected, StatusSearching, StatusFailed, StatusCancelled},
	StatusPendingApproval: {StatusApproved, StatusRejected, StatusFailed, StatusCancelled},
	StatusApproved:        {StatusCheckingOut, StatusFailed, StatusCancelled},
	StatusCheckingOut:     {StatusAwaitingPayment, StatusPendingApproval, StatusFailed, StatusCancelled},
	StatusAwaitingPayment: {StatusPaying, StatusOrdered, StatusFailed},
	StatusPaying:          {StatusOrdered, StatusFailed},
	StatusOrdered:         {},
	StatusRejected:        {},
	StatusFailed:          {},
	StatusCancelled:       {},
}

// Valid reports whether s is a known status.
func (s RequestStatus) Valid() bool { _, ok := transitions[s]; return ok }

// Terminal reports whether no further transitions are possible.
func (s RequestStatus) Terminal() bool { return s.Valid() && len(transitions[s]) == 0 }

// CanTransition reports whether from -> to is allowed by the state machine.
func CanTransition(from, to RequestStatus) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// CheckTransition returns ErrInvalidTransition (wrapped) when from -> to is not allowed.
func CheckTransition(from, to RequestStatus) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}
