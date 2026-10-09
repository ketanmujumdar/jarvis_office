package domain

import "errors"

// Sentinel errors shared across packages. Wrap them with fmt.Errorf("...: %w", err) and test with
// errors.Is. The API layer maps them to HTTP status codes (see docs/CONTRACTS.md "Errors").
var (
	ErrNotImplemented     = errors.New("not implemented")           // 501
	ErrNotFound           = errors.New("not found")                 // 404
	ErrConflict           = errors.New("conflict")                  // 409 (e.g. unique violation, stale state)
	ErrInvalidTransition  = errors.New("invalid status transition") // 409
	ErrValidation         = errors.New("validation failed")         // 400
	ErrForbidden          = errors.New("forbidden")                 // 403 (role not allowed)
	ErrUnauthorized       = errors.New("unauthorized")              // 401 (no/unknown fake token)
	ErrUpstream           = errors.New("upstream error")            // 502 (Reap/OpenAI failure)
	ErrNoActiveEnrollment = errors.New("no active card enrollment") // 409; UI should start enrollment
)
