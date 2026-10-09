// Package queue is the in-process job runner that replaces NATS for the MVP. Owner: agent E.
//
// Jobs are executed by a bounded pool of goroutines. The job contract keeps the PRD's shape
// ({v, job_id, request_id, line_item_id, kind, payload, deadline}) so a NATS-backed Runner could be
// swapped in later. Jobs must be idempotent on JobID: state lives in Postgres, not in the job.
package queue

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// Kind of a job.
type Kind string

const (
	KindParse          Kind = "request.parse"    // LLM: utterance -> line items, catalog match
	KindSearch         Kind = "search.line_item" // one line item x one vendor (Reap search+details)
	KindRank           Kind = "request.rank"     // rank offers, build basket, run policy
	KindCheckout       Kind = "request.checkout" // Reap quotes + drift check + checkouts
	KindPollCheckout   Kind = "payment.poll"     // poll GET /agentic/checkouts/:id until terminal
	KindPollEnrollment Kind = "enrollment.poll"  // poll GET /agentic/enrollments/:id until terminal
)

// Version of the job envelope.
const Version = 1

// Job is the envelope.
type Job struct {
	V          int             `json:"v"`
	JobID      string          `json:"job_id"`
	RequestID  string          `json:"request_id,omitempty"`
	LineItemID string          `json:"line_item_id,omitempty"`
	Kind       Kind            `json:"kind"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Deadline   time.Time       `json:"deadline,omitempty"` // zero = no deadline
	Attempt    int             `json:"attempt"`
}

// Result is what a handler returns; Err != nil triggers retry up to MaxAttempts if Retryable.
type Result struct {
	JobID string
	Err   error
}

// Handler processes one job kind. ctx carries the job deadline.
type Handler func(ctx context.Context, job Job) error

// Runner schedules jobs.
type Runner interface {
	// Register a handler for a kind. Must be called before Start.
	Register(kind Kind, h Handler)
	// Enqueue submits a job (fills JobID/V if empty). Non-blocking; returns error if stopped/full.
	Enqueue(ctx context.Context, job Job) (string, error)
	// EnqueueAfter schedules a job after delay (used for polling).
	EnqueueAfter(ctx context.Context, job Job, delay time.Duration) (string, error)
	// Start launches workers; returns immediately.
	Start(ctx context.Context) error
	// Stop stops accepting jobs and waits for in-flight jobs (or ctx expiry).
	Stop(ctx context.Context) error
}

// Options configure the in-process runner.
type Options struct {
	Workers     int           // default 8
	QueueSize   int           // default 1024
	MaxAttempts int           // default 3
	Backoff     time.Duration // base retry backoff, default 500ms (exponential)
	Logger      *slog.Logger  // optional; default slog.Default()
}
