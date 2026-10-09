package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsWithFakes(t *testing.T) {
	t.Setenv("FAKES", "true")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("REAP_API_KEY", "")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.OpenAIModel != "gpt-5.1" || c.OpenAIRealtimeModel != "gpt-realtime" {
		t.Errorf("models: %q %q", c.OpenAIModel, c.OpenAIRealtimeModel)
	}
	if c.ReapBaseURL != "https://sg.sandbox.api.reap.global" || c.ReapVersion != "2025-02-14" {
		t.Errorf("reap: %q %q", c.ReapBaseURL, c.ReapVersion)
	}
	if c.SearchDeadline != 20*time.Second || c.ReapCurrency != "SGD" || c.ReapCountry != "SG" {
		t.Errorf("defaults: %+v", c)
	}
}

func TestValidateRequiresKeysWithoutFakes(t *testing.T) {
	t.Setenv("FAKES", "false")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("REAP_API_KEY", "x")
	if _, err := Load(""); err == nil {
		t.Fatal("expected error for missing OPENAI_API_KEY")
	}
}

func TestDotenvDoesNotOverrideEnvAndRedacts(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "# comment\nOPENAI_API_KEY=\"from-file\"\nexport REAP_API_KEY='file-reap'\nOPENAI_MODEL=gpt-test\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKES", "false")
	t.Setenv("OPENAI_MODEL", "from-env")
	// Ensure keys are unset so the file applies; t.Setenv registers cleanup restoring previous values.
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("REAP_API_KEY", "")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("REAP_API_KEY")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.OpenAIModel != "from-env" {
		t.Errorf("env should win, got %q", c.OpenAIModel)
	}
	if c.OpenAIAPIKey != "from-file" || c.ReapAPIKey != "file-reap" {
		t.Errorf("dotenv not applied")
	}
	r := c.Redacted()
	if r.OpenAIAPIKey != "set" || r.ReapAPIKey != "set" || r.ReapWebhookSecret != "unset" {
		t.Errorf("redaction: %q %q %q", r.OpenAIAPIKey, r.ReapAPIKey, r.ReapWebhookSecret)
	}
	if got := redactDSN("postgres://u:secret@h:5432/db"); got != "postgres://u:***@h:5432/db" {
		t.Errorf("dsn redaction: %q", got)
	}
}

func TestReapReturnURLDefaults(t *testing.T) {
	cases := []struct {
		name, appURL, explicit, want string
	}{
		{"http app keeps placeholder", "http://localhost:5173", "", DefaultReapReturnURL},
		{"https app origin", "https://jarvis.example.sg/", "", "https://jarvis.example.sg/reap-return"},
		{"explicit wins", "https://jarvis.example.sg", "https://other.example/back", "https://other.example/back"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FAKES", "true")
			t.Setenv("APP_BASE_URL", tc.appURL)
			t.Setenv("REAP_RETURN_URL", tc.explicit)
			c, err := Load("")
			if err != nil {
				t.Fatal(err)
			}
			if c.ReapReturnURL != tc.want {
				t.Fatalf("return url = %q, want %q", c.ReapReturnURL, tc.want)
			}
		})
	}
}
