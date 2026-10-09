// Command api is the single Jarvis Office backend service: REST + SSE, orchestrator, in-process job
// runner (search fan-out, checkout and enrollment pollers), policy, approvals and Reap checkout.
// Wiring owner: agent F.
//
//	go run ./cmd/api              # reads ./.env if present (run from backend/)
//	FAKES=true go run ./cmd/api   # offline: in-process fake Reap server + scripted fake LLM + fake realtime minter
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/api"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/approvals"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm/fakellm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/orchestrator"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/queue"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap/fakereap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/postgres"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(envOr("DOTENV", ".env"))
	if err != nil {
		return err
	}
	// Only redacted values are logged: secrets show as "set"/"unset", the DSN password as ***.
	red := cfg.Redacted()
	log.Info("config loaded", "http_addr", cfg.HTTPAddr, "fakes", cfg.UseFakes,
		"openai_model", cfg.OpenAIModel, "realtime_model", cfg.OpenAIRealtimeModel,
		"openai_api_key", red.OpenAIAPIKey, "reap_api_key", red.ReapAPIKey,
		"reap_base_url", cfg.ReapBaseURL, "reap_simulate_checkout", cfg.ReapSimulateCheckout,
		"reap_webhooks", cfg.ReapWebhooksEnabled, "db", red.DatabaseURL, "cors", cfg.CORSOrigins)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cfg.AutoMigrate {
		applied, err := store.Migrate(ctx, pool)
		if err != nil {
			return err
		}
		log.Info("migrations", "applied", applied)
	}
	st := postgres.New(pool)
	if cfg.AutoSeed {
		if err := seed.Load(ctx, st, cfg.SeedDir, seed.Options{}); err != nil {
			log.Warn("seed", "err", err)
		}
	}

	a, err := build(cfg, st, log)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.Start(ctx); err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr: cfg.HTTPAddr, Handler: a.Handler,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
		// No WriteTimeout: SSE streams are long-lived (the handler sends heartbeats).
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	return a.Stop(shutdownCtx)
}

// app is the wired service, independent of how the store was opened (tests pass memstore).
type app struct {
	Handler   http.Handler
	Runner    queue.Runner
	Orch      *orchestrator.Impl
	Approvals *approvals.Impl
	Bus       *events.Memory
	Reap      reap.Client
	FakeReap  *fakereap.Server // non-nil when cfg.UseFakes
	log       *slog.Logger
	closers   []func()
}

// build wires every collaborator. With cfg.UseFakes it starts an in-process fake Reap server
// (hosted enrollment/checkout pages work by opening their URLs) and uses the scripted fake LLM and
// fake realtime minter, so the whole flow runs offline.
func build(cfg config.Config, st store.Store, log *slog.Logger) (*app, error) {
	if st == nil {
		return nil, fmt.Errorf("build: store is required")
	}
	a := &app{log: log, Bus: events.NewMemory(256)}
	auditLog := audit.New(st)
	runner := queue.NewInProcess(queue.Options{Workers: cfg.QueueWorkers, Logger: log})
	a.Runner = runner

	var (
		llmClient llm.Client
		minter    realtime.Minter
	)
	if cfg.UseFakes {
		fr := fakereap.New(fakereap.Options{AllowHTTPReturnURL: true, AutoApproveCheckouts: cfg.ReapSimulateCheckout})
		a.FakeReap = fr
		a.closers = append(a.closers, fr.Close)
		a.Reap = fr.Client()
		llmClient = fakellm.New()
		minter = realtime.NewFake()
		log.Warn("FAKES=true: using fake Reap, fake LLM and fake realtime minter", "fake_reap_url", fr.URL)
	} else {
		a.Reap = reap.NewHTTPClient(reap.Options{
			BaseURL: cfg.ReapBaseURL, APIKey: cfg.ReapAPIKey, Version: cfg.ReapVersion,
			SimulateCheckout: cfg.ReapSimulateCheckout, Timeout: cfg.ReapTimeout,
		})
		llmClient = llm.NewOpenAI(llm.OpenAIOptions{
			APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIModel, Timeout: cfg.LLMTimeout,
		})
		minter = realtime.NewOpenAIMinter(realtime.OpenAIOptions{
			APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIRealtimeModel, Voice: cfg.OpenAIRealtimeVoice,
		})
	}

	if !cfg.UseFakes && cfg.ReapReturnURL == config.DefaultReapReturnURL {
		log.Warn("REAP_RETURN_URL is the example.com placeholder: after approving on Reap users land outside the app; set it to <app https origin>/reap-return")
	}
	appr := approvals.New(approvals.Deps{Store: st, Events: a.Bus})
	orch := orchestrator.New(orchestrator.Deps{
		Store: st, Queue: runner, Bus: a.Bus, Audit: auditLog, Approvals: appr, Reap: a.Reap,
		Parser: agents.NewLLMParser(llmClient), Adapter: agents.NewReapAdapter(a.Reap),
		Clock: time.Now, SearchDeadline: cfg.SearchDeadline, QuoteProbeTimeout: cfg.QuoteProbeTimeout, ResultsPerVendor: cfg.SearchResultsPerVendor,
		CheckoutPollInterval: cfg.CheckoutPollInterval, CheckoutPollTimeout: cfg.CheckoutPollTimeout,
		ReapReturnURL: cfg.ReapReturnURL, Logger: log,
		Matcher: orchestrator.LLMOfferMatcher(llmClient),
	})
	appr.SetHandler(orch)
	a.Orch, a.Approvals = orch, appr

	tools := agents.NewTools(orch.Tools(), st, auditLog)
	chat := &agents.ChatAgent{LLM: llmClient, Store: st, Tools: tools, Audit: auditLog, Model: cfg.OpenAIModel}

	srv := api.NewServer(api.Deps{
		Config: cfg, Store: st, Orchestrator: orch, Approvals: appr, Agent: chat, Tools: tools,
		Realtime: minter, Bus: a.Bus, Audit: auditLog, Logger: log,
	})
	a.Handler = srv.Handler()
	return a, nil
}

// Start launches the job runner and resumes in-flight work (search, checkout and enrollment polls).
func (a *app) Start(ctx context.Context) error {
	if err := a.Runner.Start(ctx); err != nil {
		return err
	}
	if err := a.Orch.Resume(ctx); err != nil {
		a.log.Warn("resume", "err", err)
	}
	return nil
}

// Stop drains the runner.
func (a *app) Stop(ctx context.Context) error { return a.Runner.Stop(ctx) }

// Close releases fakes.
func (a *app) Close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
