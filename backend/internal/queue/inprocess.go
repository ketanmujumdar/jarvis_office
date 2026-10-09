package queue

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Errors returned by the in-process runner.
var (
	ErrStopped     = errors.New("queue: runner stopped")
	ErrQueueFull   = errors.New("queue: queue full")
	ErrUnknownKind = errors.New("queue: no handler registered for kind")
)

// permanentError marks a handler error that must not be retried.
type permanentError struct{ err error }

func (p permanentError) Error() string { return p.err.Error() }
func (p permanentError) Unwrap() error { return p.err }

// Permanent wraps err so the runner does not retry the job. Permanent(nil) is nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err (or anything it wraps) was marked with Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

type jobState int

const (
	statePending jobState = iota + 1 // queued or scheduled (EnqueueAfter / retry backoff)
	stateRunning
	stateDone
)

const doneMemory = 10000 // how many completed job ids are remembered for idempotency

// InProcess is the goroutine-backed Runner: a bounded pool of workers reading a buffered channel.
//
//   - Idempotent on JobID: enqueueing an id that is pending, running or already completed is a
//     no-op that returns the same id. A job that failed for good may be enqueued again.
//   - Per-job deadline: Job.Deadline becomes the handler context deadline.
//   - Retries: a failing handler is retried (same JobID, Attempt+1) with exponential backoff up to
//     MaxAttempts, unless the error is Permanent or the job deadline has passed.
//   - Panics in handlers are recovered and treated as errors.
type InProcess struct {
	opts Options
	log  *slog.Logger

	mu        sync.Mutex
	handlers  map[Kind]Handler
	ch        chan Job
	state     map[string]jobState
	doneRing  []string
	doneNext  int
	timers    map[*time.Timer]timerEntry
	inflight  int // queued + running + retry backoff (what WaitIdle waits for)
	scheduled int // EnqueueAfter jobs whose delay has not elapsed (not waited for)
	started   bool
	stopped   bool
	baseCtx   context.Context
	cancel    context.CancelFunc
	workersWG sync.WaitGroup
}

var _ Runner = (*InProcess)(nil)

// NewInProcess returns the goroutine-backed Runner.
func NewInProcess(opts Options) *InProcess {
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 1024
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	if opts.Backoff <= 0 {
		opts.Backoff = 500 * time.Millisecond
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &InProcess{
		opts:     opts,
		log:      log,
		handlers: map[Kind]Handler{},
		ch:       make(chan Job, opts.QueueSize),
		state:    map[string]jobState{},
		doneRing: make([]string, doneMemory),
		timers:   map[*time.Timer]timerEntry{},
	}
}

// Register a handler for a kind.
func (r *InProcess) Register(kind Kind, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[kind] = h
}

// Enqueue submits a job. Jobs enqueued before Start are buffered.
func (r *InProcess) Enqueue(ctx context.Context, job Job) (string, error) {
	return r.submit(job, 0)
}

// EnqueueAfter schedules a job after delay.
func (r *InProcess) EnqueueAfter(ctx context.Context, job Job, delay time.Duration) (string, error) {
	return r.submit(job, delay)
}

func (r *InProcess) submit(job Job, delay time.Duration) (string, error) {
	if job.V == 0 {
		job.V = Version
	}
	if job.JobID == "" {
		job.JobID = NewID()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return "", ErrStopped
	}
	if _, ok := r.handlers[job.Kind]; !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownKind, job.Kind)
	}
	if st, ok := r.state[job.JobID]; ok && st != 0 {
		return job.JobID, nil // idempotent: already pending, running or done
	}
	r.state[job.JobID] = statePending
	if delay > 0 {
		r.scheduled++
		r.scheduleLocked(job, delay, false)
		return job.JobID, nil
	}
	r.inflight++
	select {
	case r.ch <- job:
		return job.JobID, nil
	default:
		delete(r.state, job.JobID)
		r.inflight--
		return "", ErrQueueFull
	}
}

// scheduleLocked pushes job onto the channel after delay. Caller holds r.mu and has already
// accounted the job (as inflight when retry is true, else as scheduled).
func (r *InProcess) scheduleLocked(job Job, delay time.Duration, retry bool) {
	var t *time.Timer
	t = time.AfterFunc(delay, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.timers[t]; !ok {
			return // cancelled by Stop
		}
		delete(r.timers, t)
		if !retry {
			r.scheduled--
			r.inflight++
		}
		if r.stopped {
			r.dropLocked(job.JobID)
			return
		}
		select {
		case r.ch <- job:
		default:
			r.log.Warn("queue full, dropping scheduled job", "job_id", job.JobID, "kind", job.Kind)
			r.dropLocked(job.JobID)
		}
	})
	r.timers[t] = timerEntry{id: job.JobID, retry: retry}
}

type timerEntry struct {
	id    string
	retry bool
}

func (r *InProcess) dropLocked(id string) {
	if r.state[id] == statePending || r.state[id] == stateRunning {
		r.inflight--
	}
	delete(r.state, id)
}

// Start launches workers. The handler context is detached from ctx so in-flight jobs are not
// cancelled by the caller's shutdown signal; Stop bounds how long they may keep running.
func (r *InProcess) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	if r.stopped {
		return ErrStopped
	}
	r.started = true
	r.baseCtx, r.cancel = context.WithCancel(context.WithoutCancel(ctx))
	for i := 0; i < r.opts.Workers; i++ {
		r.workersWG.Add(1)
		go r.worker()
	}
	return nil
}

// Stop stops accepting jobs, cancels scheduled ones, and waits for queued and running jobs to
// finish or for ctx to expire (then the handlers' contexts are cancelled).
func (r *InProcess) Stop(ctx context.Context) error {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return nil
	}
	r.stopped = true
	for t, te := range r.timers {
		t.Stop()
		delete(r.timers, t)
		if !te.retry {
			r.scheduled--
			delete(r.state, te.id)
			continue
		}
		r.dropLocked(te.id)
	}
	started := r.started
	close(r.ch)
	r.mu.Unlock()
	if !started {
		return nil
	}
	done := make(chan struct{})
	go func() { r.workersWG.Wait(); close(done) }()
	select {
	case <-done:
		r.cancel()
		return nil
	case <-ctx.Done():
		r.cancel()
		<-done
		return ctx.Err()
	}
}

// WaitIdle blocks until no job is queued, running or waiting for a retry, or ctx expires. Jobs
// scheduled with EnqueueAfter are not waited for until their delay elapses (so a self-rescheduling
// poller does not keep the runner busy forever). Intended for tests and graceful drains.
func (r *InProcess) WaitIdle(ctx context.Context) error {
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		r.mu.Lock()
		n := r.inflight
		r.mu.Unlock()
		if n == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("queue: wait idle (%d in flight): %w", n, ctx.Err())
		case <-tick.C:
		}
	}
}

// Status reports the runner's view of a job id: "pending", "running", "done" or "" (unknown or
// failed for good).
func (r *InProcess) Status(jobID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.state[jobID] {
	case statePending:
		return "pending"
	case stateRunning:
		return "running"
	case stateDone:
		return "done"
	}
	return ""
}

func (r *InProcess) worker() {
	defer r.workersWG.Done()
	for job := range r.ch {
		r.run(job)
	}
}

func (r *InProcess) run(job Job) {
	r.mu.Lock()
	h := r.handlers[job.Kind]
	r.state[job.JobID] = stateRunning
	base := r.baseCtx
	r.mu.Unlock()

	ctx, cancel := base, context.CancelFunc(func() {})
	if !job.Deadline.IsZero() {
		ctx, cancel = context.WithDeadline(base, job.Deadline)
	}
	err := safeCall(ctx, h, job)
	cancel()

	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.markDoneLocked(job.JobID)
		return
	}
	expired := !job.Deadline.IsZero() && !time.Now().Before(job.Deadline)
	retry := !IsPermanent(err) && !expired && job.Attempt+1 < r.opts.MaxAttempts && !r.stopped
	r.log.Warn("job failed", "job_id", job.JobID, "kind", job.Kind, "request_id", job.RequestID,
		"attempt", job.Attempt, "retry", retry, "err", err)
	if !retry {
		r.dropLocked(job.JobID) // failed for good: may be enqueued again
		return
	}
	r.state[job.JobID] = statePending
	delay := r.opts.Backoff << job.Attempt
	job.Attempt++
	r.scheduleLocked(job, delay, true)
}

func (r *InProcess) markDoneLocked(id string) {
	r.inflight--
	r.state[id] = stateDone
	if old := r.doneRing[r.doneNext]; old != "" && r.state[old] == stateDone {
		delete(r.state, old)
	}
	r.doneRing[r.doneNext] = id
	r.doneNext = (r.doneNext + 1) % len(r.doneRing)
}

func safeCall(ctx context.Context, h Handler, job Job) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("queue: handler panic: %v", p)
		}
	}()
	return h(ctx, job)
}

// NewID returns a random RFC 4122 version 4 UUID string.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Scheduled reports how many EnqueueAfter jobs are waiting for their delay to elapse.
func (r *InProcess) Scheduled() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scheduled
}
