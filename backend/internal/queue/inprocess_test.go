package queue

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestRunner(t *testing.T, opts Options) *InProcess {
	t.Helper()
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Backoff == 0 {
		opts.Backoff = time.Millisecond
	}
	r := NewInProcess(opts)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.Stop(ctx)
	})
	return r
}

func waitIdle(t *testing.T, r *InProcess) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueRunsHandlerAndFillsEnvelope(t *testing.T) {
	r := newTestRunner(t, Options{Workers: 2})
	var got Job
	var mu sync.Mutex
	r.Register(KindParse, func(ctx context.Context, j Job) error {
		mu.Lock()
		got = j
		mu.Unlock()
		return nil
	})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	id, err := r.Enqueue(context.Background(), Job{Kind: KindParse, RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, r)
	mu.Lock()
	defer mu.Unlock()
	if got.JobID != id || len(id) != 36 || got.V != Version || got.RequestID != "r1" {
		t.Fatalf("unexpected job %+v (id %q)", got, id)
	}
	if r.Status(id) != "done" {
		t.Fatalf("status = %q", r.Status(id))
	}
}

func TestIdempotentOnJobID(t *testing.T) {
	cases := []struct {
		name      string
		afterDone bool
	}{
		{"duplicate while pending", false},
		{"duplicate after completion", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRunner(t, Options{Workers: 1})
			var calls atomic.Int32
			r.Register(KindSearch, func(ctx context.Context, j Job) error { calls.Add(1); return nil })
			job := Job{JobID: "fixed-id", Kind: KindSearch}
			if _, err := r.Enqueue(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			if tc.afterDone {
				_ = r.Start(context.Background())
				waitIdle(t, r)
			}
			id, err := r.Enqueue(context.Background(), job)
			if err != nil || id != "fixed-id" {
				t.Fatalf("second enqueue: %q %v", id, err)
			}
			_ = r.Start(context.Background())
			waitIdle(t, r)
			if n := calls.Load(); n != 1 {
				t.Fatalf("handler ran %d times, want 1", n)
			}
		})
	}
}

func TestRetries(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name        string
		maxAttempts int
		failTimes   int
		err         error
		wantCalls   int32
		wantStatus  string
	}{
		{"succeeds after retries", 3, 2, boom, 3, "done"},
		{"gives up after max attempts", 3, 10, boom, 3, ""},
		{"permanent error is not retried", 3, 10, Permanent(boom), 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRunner(t, Options{Workers: 1, MaxAttempts: tc.maxAttempts})
			var calls atomic.Int32
			var attempts []int
			var mu sync.Mutex
			r.Register(KindCheckout, func(ctx context.Context, j Job) error {
				mu.Lock()
				attempts = append(attempts, j.Attempt)
				mu.Unlock()
				if int(calls.Add(1)) <= tc.failTimes {
					return tc.err
				}
				return nil
			})
			_ = r.Start(context.Background())
			id, _ := r.Enqueue(context.Background(), Job{Kind: KindCheckout})
			waitIdle(t, r)
			if calls.Load() != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", calls.Load(), tc.wantCalls)
			}
			if s := r.Status(id); s != tc.wantStatus {
				t.Fatalf("status = %q, want %q", s, tc.wantStatus)
			}
			mu.Lock()
			for i, a := range attempts {
				if a != i {
					t.Fatalf("attempts = %v", attempts)
				}
			}
			mu.Unlock()
		})
	}
}

func TestFailedJobCanBeEnqueuedAgain(t *testing.T) {
	r := newTestRunner(t, Options{Workers: 1, MaxAttempts: 1})
	var calls atomic.Int32
	r.Register(KindRank, func(ctx context.Context, j Job) error {
		if calls.Add(1) == 1 {
			return errors.New("first fails")
		}
		return nil
	})
	_ = r.Start(context.Background())
	_, _ = r.Enqueue(context.Background(), Job{JobID: "j", Kind: KindRank})
	waitIdle(t, r)
	_, _ = r.Enqueue(context.Background(), Job{JobID: "j", Kind: KindRank})
	waitIdle(t, r)
	if calls.Load() != 2 || r.Status("j") != "done" {
		t.Fatalf("calls=%d status=%q", calls.Load(), r.Status("j"))
	}
}

func TestPerJobDeadline(t *testing.T) {
	r := newTestRunner(t, Options{Workers: 1, MaxAttempts: 3})
	var calls atomic.Int32
	var sawDeadline atomic.Bool
	r.Register(KindSearch, func(ctx context.Context, j Job) error {
		calls.Add(1)
		if _, ok := ctx.Deadline(); ok {
			sawDeadline.Store(true)
		}
		<-ctx.Done()
		return ctx.Err()
	})
	_ = r.Start(context.Background())
	_, _ = r.Enqueue(context.Background(), Job{Kind: KindSearch, Deadline: time.Now().Add(20 * time.Millisecond)})
	waitIdle(t, r)
	if !sawDeadline.Load() {
		t.Fatal("handler context had no deadline")
	}
	if calls.Load() != 1 {
		t.Fatalf("expired job retried: calls=%d", calls.Load())
	}
}

func TestBoundedConcurrency(t *testing.T) {
	const workers = 3
	r := newTestRunner(t, Options{Workers: workers})
	var cur, peak atomic.Int32
	r.Register(KindSearch, func(ctx context.Context, j Job) error {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		cur.Add(-1)
		return nil
	})
	_ = r.Start(context.Background())
	for i := 0; i < 20; i++ {
		if _, err := r.Enqueue(context.Background(), Job{Kind: KindSearch}); err != nil {
			t.Fatal(err)
		}
	}
	waitIdle(t, r)
	if peak.Load() > workers || peak.Load() < 2 {
		t.Fatalf("peak concurrency %d, want 2..%d", peak.Load(), workers)
	}
}

func TestEnqueueAfterDelays(t *testing.T) {
	r := newTestRunner(t, Options{Workers: 1})
	ran := make(chan time.Time, 1)
	r.Register(KindPollCheckout, func(ctx context.Context, j Job) error { ran <- time.Now(); return nil })
	_ = r.Start(context.Background())
	start := time.Now()
	if _, err := r.EnqueueAfter(context.Background(), Job{Kind: KindPollCheckout}, 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case at := <-ran:
		if at.Sub(start) < 25*time.Millisecond {
			t.Fatalf("ran too early: %v", at.Sub(start))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled job never ran")
	}
}

func TestPanicIsRecovered(t *testing.T) {
	r := newTestRunner(t, Options{Workers: 1, MaxAttempts: 1})
	r.Register(KindParse, func(ctx context.Context, j Job) error { panic("kaboom") })
	var ok atomic.Bool
	r.Register(KindRank, func(ctx context.Context, j Job) error { ok.Store(true); return nil })
	_ = r.Start(context.Background())
	_, _ = r.Enqueue(context.Background(), Job{Kind: KindParse})
	_, _ = r.Enqueue(context.Background(), Job{Kind: KindRank})
	waitIdle(t, r)
	if !ok.Load() {
		t.Fatal("worker died after panic")
	}
}

func TestErrors(t *testing.T) {
	r := newTestRunner(t, Options{Workers: 1, QueueSize: 1})
	r.Register(KindParse, func(ctx context.Context, j Job) error { return nil })
	if _, err := r.Enqueue(context.Background(), Job{Kind: "nope"}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := r.Enqueue(context.Background(), Job{Kind: KindParse}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Enqueue(context.Background(), Job{Kind: KindParse}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("full queue: %v", err)
	}
	_ = r.Start(context.Background())
	waitIdle(t, r)
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Enqueue(context.Background(), Job{Kind: KindParse}); !errors.Is(err, ErrStopped) {
		t.Fatalf("after stop: %v", err)
	}
}

func TestStopCancelsScheduledAndWaitsForRunning(t *testing.T) {
	r := newTestRunner(t, Options{Workers: 1})
	var finished atomic.Bool
	started := make(chan struct{})
	r.Register(KindCheckout, func(ctx context.Context, j Job) error {
		close(started)
		time.Sleep(20 * time.Millisecond)
		finished.Store(true)
		return nil
	})
	var scheduledRan atomic.Bool
	r.Register(KindPollCheckout, func(ctx context.Context, j Job) error { scheduledRan.Store(true); return nil })
	_ = r.Start(context.Background())
	_, _ = r.Enqueue(context.Background(), Job{Kind: KindCheckout})
	_, _ = r.EnqueueAfter(context.Background(), Job{Kind: KindPollCheckout}, time.Hour)
	<-started
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !finished.Load() {
		t.Fatal("Stop returned before the running job finished")
	}
	if scheduledRan.Load() {
		t.Fatal("scheduled job ran after stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNewIDFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID()
		if len(id) != 36 || id[14] != '4' || seen[id] {
			t.Fatalf("bad id %q", id)
		}
		seen[id] = true
	}
}
