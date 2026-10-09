// Package reaptest is a thin alias for reap/fakereap (the name used in docs/CONTRACTS.md). Prefer
// importing fakereap directly; this package exists so either import path works.
package reaptest

import "github.com/ketanmujumdar/jarvis_office/backend/internal/reap/fakereap"

type (
	Server          = fakereap.Server
	Options         = fakereap.Options
	Fault           = fakereap.Fault
	RecordedRequest = fakereap.RecordedRequest
	FixtureProduct  = fakereap.FixtureProduct
	FixtureVariant  = fakereap.FixtureVariant
)

// New starts a fake Reap server (see fakereap.New).
func New(opts Options) *Server { return fakereap.New(opts) }

// DefaultProducts returns the fixture catalogue.
func DefaultProducts() []FixtureProduct { return fakereap.DefaultProducts() }
