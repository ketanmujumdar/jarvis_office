package harness

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// BuildAPI compiles backend/cmd/api into outDir and returns the binary path.
func BuildAPI(ctx context.Context, backendDir, outDir string) (string, error) {
	bin := filepath.Join(outDir, "jarvis-api")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/api")
	cmd.Dir = backendDir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return "", errf("go build ./cmd/api: %v\n%s", err, out.String())
	}
	return bin, nil
}

// FreePort returns a free localhost TCP port.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// API is a running api process.
type API struct {
	BaseURL string
	cmd     *exec.Cmd
	logs    *lockedBuffer
	exited  chan struct{}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// scrubbedEnv is os.Environ() minus everything the api reads, so the parent's real keys (or a
// developer's exported DATABASE_URL) can never leak into the e2e process.
func scrubbedEnv() []string {
	drop := []string{"OPENAI_", "REAP_", "DATABASE_URL", "FAKES", "HTTP_ADDR", "SEED_DIR", "AUTO_", "DOTENV",
		"SEARCH_", "CHECKOUT_", "QUEUE_", "LLM_", "CORS_", "APP_BASE_URL"}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		skip := false
		for _, p := range drop {
			if strings.HasPrefix(k, p) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, kv)
		}
	}
	return out
}

// StartAPI runs bin with env (KEY=VALUE) in workDir and waits until /healthz answers 200.
// HTTP_ADDR is chosen automatically. DOTENV points at a file that does not exist so that
// backend/.env (real keys) is never loaded.
func StartAPI(ctx context.Context, bin, workDir string, env map[string]string, ready time.Duration) (*API, error) {
	port, err := FreePort()
	if err != nil {
		return nil, err
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	cmd := exec.Command(bin)
	cmd.Dir = workDir
	cmd.Env = append(scrubbedEnv(), "HTTP_ADDR="+addr, "DOTENV="+filepath.Join(workDir, "no-such.env"))
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	logs := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	a := &API{BaseURL: "http://" + addr, cmd: cmd, logs: logs, exited: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(a.exited) }()

	deadline := time.Now().Add(ready)
	hc := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-a.exited:
			return nil, errf("api exited during startup:\n%s", logs.String())
		case <-ctx.Done():
			a.Stop()
			return nil, ctx.Err()
		default:
		}
		resp, err := hc.Get(a.BaseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return a, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	a.Stop()
	return nil, errf("api not healthy after %s:\n%s", ready, logs.String())
}

// Exited reports whether the process has ended (crash or stop).
func (a *API) Exited() bool {
	select {
	case <-a.exited:
		return true
	default:
		return false
	}
}

// Logs returns everything the process wrote so far.
func (a *API) Logs() string { return a.logs.String() }

// Stop sends SIGINT, then kills after 10s.
func (a *API) Stop() {
	if a == nil || a.cmd.Process == nil {
		return
	}
	_ = a.cmd.Process.Signal(os.Interrupt)
	select {
	case <-a.exited:
	case <-time.After(10 * time.Second):
		_ = a.cmd.Process.Kill()
		<-a.exited
	}
}

// ResetDatabase connects to baseDSN, drops and recreates database name (terminating sessions),
// and returns a DSN for it. baseDSN's own database is never modified.
func ResetDatabase(ctx context.Context, baseDSN, name string) (string, error) {
	if !validIdent(name) {
		return "", errf("invalid database name %q", name)
	}
	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		return "", errf("connect %s: %w", redactDSN(baseDSN), err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
		return "", err
	}
	if _, err := conn.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		return "", err
	}
	u, err := url.Parse(baseDSN)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

// DropDatabase removes a database created by ResetDatabase.
func DropDatabase(ctx context.Context, baseDSN, name string) error {
	if !validIdent(name) {
		return errf("invalid database name %q", name)
	}
	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	return err
}

func validIdent(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn
	}
	if _, ok := u.User.Password(); ok {
		u.User = url.UserPassword(u.User.Username(), "xxx")
	}
	return u.String()
}
