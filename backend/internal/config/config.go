// Package config loads runtime configuration from the environment, optionally seeded from a
// dotenv file (backend/.env). Secret values are never logged: use Config.Redacted() for logs.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full runtime configuration of cmd/api.
type Config struct {
	HTTPAddr    string   // HTTP_ADDR, default ":8080"
	DatabaseURL string   // DATABASE_URL, default postgres://jarvis:jarvis@localhost:5432/jarvis?sslmode=disable
	CORSOrigins []string // CORS_ORIGINS comma-separated, default "*"
	SeedDir     string   // SEED_DIR, default "seed" (relative to backend/)
	AutoMigrate bool     // AUTO_MIGRATE, default true: run migrations on boot
	AutoSeed    bool     // AUTO_SEED, default true: seed if tables are empty
	// UseFakes runs with in-memory fake Reap + LLM (offline demo / e2e). FAKES=true.
	UseFakes bool

	// OpenAI
	OpenAIAPIKey        string        // OPENAI_API_KEY (secret)
	OpenAIBaseURL       string        // OPENAI_BASE_URL, default https://api.openai.com/v1
	OpenAIModel         string        // OPENAI_MODEL, default gpt-5.1
	OpenAIRealtimeModel string        // OPENAI_REALTIME_MODEL, default gpt-realtime
	OpenAIRealtimeVoice string        // OPENAI_REALTIME_VOICE, default marin
	LLMTimeout          time.Duration // LLM_TIMEOUT, default 60s

	// Reap
	ReapAPIKey           string        // REAP_API_KEY (secret)
	ReapBaseURL          string        // REAP_BASE_URL, default https://sg.sandbox.api.reap.global
	ReapVersion          string        // REAP_VERSION, default 2025-02-14
	ReapCountry          string        // REAP_COUNTRY, default SG
	ReapCurrency         string        // REAP_CURRENCY, default SGD
	ReapSimulateCheckout bool          // REAP_SIMULATE_CHECKOUT, default false: send X-Simulate-Checkout: COMPLETED (sandbox only)
	ReapReturnURL        string        // REAP_RETURN_URL: HTTPS URL Reap redirects to after hosted pages
	ReapWebhooksEnabled  bool          // REAP_WEBHOOKS_ENABLED, default false (polling is the default)
	ReapWebhookSecret    string        // REAP_WEBHOOK_SECRET (secret)
	ReapTimeout          time.Duration // REAP_TIMEOUT, default 30s

	// Orchestration
	SearchDeadline         time.Duration // SEARCH_DEADLINE, default 20s (per request fan-out)
	SearchResultsPerVendor int           // SEARCH_RESULTS_PER_VENDOR, default 5
	CheckoutPollInterval   time.Duration // CHECKOUT_POLL_INTERVAL, default 3s
	CheckoutPollTimeout    time.Duration // CHECKOUT_POLL_TIMEOUT, default 30m
	QueueWorkers           int           // QUEUE_WORKERS, default 8

	AppBaseURL string // APP_BASE_URL, default http://localhost:5173 (Flutter web dev server)
}

// Load reads the environment. If dotenvPath is non-empty and exists, its KEY=VALUE lines are applied
// first for keys not already set in the process environment.
func Load(dotenvPath string) (Config, error) {
	if dotenvPath != "" {
		if err := applyDotenv(dotenvPath); err != nil && !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("load %s: %w", dotenvPath, err)
		}
	}
	c := Config{
		HTTPAddr:               env("HTTP_ADDR", ":8080"),
		DatabaseURL:            env("DATABASE_URL", "postgres://jarvis:jarvis@localhost:5432/jarvis?sslmode=disable"),
		CORSOrigins:            splitList(env("CORS_ORIGINS", "*")),
		SeedDir:                env("SEED_DIR", "seed"),
		AutoMigrate:            envBool("AUTO_MIGRATE", true),
		AutoSeed:               envBool("AUTO_SEED", true),
		UseFakes:               envBool("FAKES", false),
		OpenAIAPIKey:           os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:          env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIModel:            env("OPENAI_MODEL", "gpt-5.1"),
		OpenAIRealtimeModel:    env("OPENAI_REALTIME_MODEL", "gpt-realtime"),
		OpenAIRealtimeVoice:    env("OPENAI_REALTIME_VOICE", "marin"),
		LLMTimeout:             envDuration("LLM_TIMEOUT", 60*time.Second),
		ReapAPIKey:             os.Getenv("REAP_API_KEY"),
		ReapBaseURL:            env("REAP_BASE_URL", "https://sg.sandbox.api.reap.global"),
		ReapVersion:            env("REAP_VERSION", "2025-02-14"),
		ReapCountry:            env("REAP_COUNTRY", "SG"),
		ReapCurrency:           env("REAP_CURRENCY", "SGD"),
		ReapSimulateCheckout:   envBool("REAP_SIMULATE_CHECKOUT", false),
		ReapReturnURL:          env("REAP_RETURN_URL", DefaultReapReturnURL),
		ReapWebhooksEnabled:    envBool("REAP_WEBHOOKS_ENABLED", false),
		ReapWebhookSecret:      os.Getenv("REAP_WEBHOOK_SECRET"),
		ReapTimeout:            envDuration("REAP_TIMEOUT", 30*time.Second),
		SearchDeadline:         envDuration("SEARCH_DEADLINE", 20*time.Second),
		SearchResultsPerVendor: envInt("SEARCH_RESULTS_PER_VENDOR", 5),
		CheckoutPollInterval:   envDuration("CHECKOUT_POLL_INTERVAL", 3*time.Second),
		CheckoutPollTimeout:    envDuration("CHECKOUT_POLL_TIMEOUT", 30*time.Minute),
		QueueWorkers:           envInt("QUEUE_WORKERS", 8),
		AppBaseURL:             env("APP_BASE_URL", "http://localhost:5173"),
	}
	// Reap requires an HTTPS return URL. When the app itself is served over HTTPS, send the user
	// back to the app's /reap-return route (it refreshes the card / payment state).
	if os.Getenv("REAP_RETURN_URL") == "" && strings.HasPrefix(c.AppBaseURL, "https://") {
		c.ReapReturnURL = strings.TrimRight(c.AppBaseURL, "/") + "/reap-return"
	}
	return c, c.Validate()
}

// DefaultReapReturnURL is the placeholder return URL used when neither REAP_RETURN_URL nor an
// HTTPS APP_BASE_URL is set. After approving on Reap the browser lands there, not in the app.
const DefaultReapReturnURL = "https://example.com/jarvis/reap-return"

// Validate checks invariants. Missing API keys are allowed when UseFakes is true.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if !c.UseFakes {
		if c.OpenAIAPIKey == "" {
			return fmt.Errorf("OPENAI_API_KEY is required (or set FAKES=true)")
		}
		if c.ReapAPIKey == "" {
			return fmt.Errorf("REAP_API_KEY is required (or set FAKES=true)")
		}
	}
	if c.ReapWebhooksEnabled && c.ReapWebhookSecret == "" {
		return fmt.Errorf("REAP_WEBHOOK_SECRET is required when REAP_WEBHOOKS_ENABLED=true")
	}
	return nil
}

// Redacted returns a copy safe to log (secrets replaced by "set"/"unset").
func (c Config) Redacted() Config {
	r := c
	r.OpenAIAPIKey = redact(c.OpenAIAPIKey)
	r.ReapAPIKey = redact(c.ReapAPIKey)
	r.ReapWebhookSecret = redact(c.ReapWebhookSecret)
	r.DatabaseURL = redactDSN(c.DatabaseURL)
	return r
}

func redact(s string) string {
	if s == "" {
		return "unset"
	}
	return "set"
}

func redactDSN(dsn string) string {
	// postgres://user:pass@host -> postgres://user:***@host
	at := strings.LastIndex(dsn, "@")
	scheme := strings.Index(dsn, "://")
	if at < 0 || scheme < 0 {
		return dsn
	}
	creds := dsn[scheme+3 : at]
	if i := strings.Index(creds, ":"); i >= 0 {
		return dsn[:scheme+3] + creds[:i] + ":***" + dsn[at:]
	}
	return dsn
}

func applyDotenv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			_ = os.Setenv(k, v)
		}
	}
	return sc.Err()
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func envInt(k string, def int) int {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
