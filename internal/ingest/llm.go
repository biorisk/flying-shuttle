package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// ErrCompleterNotReady mirrors ErrEmbedderNotReady for the instruct LLM.
var ErrCompleterNotReady = errors.New("instruct LLM not ready")

// HTTPCompleter speaks the JSON protocol of python/llm_server.py:
//
//	GET  /health                                 -> 200 once loaded
//	POST /complete {"system":..,"user":..}       -> {"text": ".."}
type HTTPCompleter struct {
	BaseURL string
	Client  *http.Client
}

// NewHTTPCompleter returns a client for baseURL (e.g. "http://127.0.0.1:8072").
func NewHTTPCompleter(baseURL string) *HTTPCompleter {
	return &HTTPCompleter{BaseURL: baseURL, Client: &http.Client{Timeout: 3 * time.Minute}}
}

func (c *HTTPCompleter) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// Complete implements the stitch/search Completer interface.
func (c *HTTPCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(map[string]string{"system": system, "user": user})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/complete", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCompleterNotReady, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return "", ErrCompleterNotReady
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return "", fmt.Errorf("complete: status %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("complete: decode: %w", err)
	}
	return out.Text, nil
}

// PythonCompleterConfig configures the managed instruct-LLM subprocess.
type PythonCompleterConfig struct {
	Python  string
	Script  string
	Addr    string
	Dir     string
	Threads int
	// StartTimeout bounds how long Complete waits for the model to load on a
	// cold start. 0 -> 90s.
	StartTimeout time.Duration
}

// PythonCompleter starts python/llm_server.py on demand, proxies Complete calls
// to it, and lets it idle-shed itself. It shares a ComputeGate with the
// embedder so a digest pass and an embed batch never run large jobs together.
type PythonCompleter struct {
	cfg  PythonCompleterConfig
	http *HTTPCompleter
	Gate *ComputeGate

	mu      sync.Mutex // guards cmd/running and serialises Complete (single-flight)
	cmd     *exec.Cmd
	running bool
}

// NewPythonCompleter builds a supervisor. Nothing spawns until the first
// Complete call.
func NewPythonCompleter(cfg PythonCompleterConfig) *PythonCompleter {
	return &PythonCompleter{cfg: cfg, http: NewHTTPCompleter("http://" + cfg.Addr)}
}

func (p *PythonCompleter) startTimeout() time.Duration {
	if p.cfg.StartTimeout > 0 {
		return p.cfg.StartTimeout
	}
	return 90 * time.Second
}

// Complete implements the Completer interface. It ensures the subprocess is up
// (cold-starting it if needed), then makes one gated, single-flight call.
func (p *PythonCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	if _, err := os.Stat(p.cfg.Script); err != nil {
		return "", fmt.Errorf("%w: %s missing", ErrCompleterNotReady, p.cfg.Script)
	}
	if err := p.Gate.Acquire(ctx); err != nil {
		return "", err
	}
	defer p.Gate.Release()

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureUpLocked(ctx); err != nil {
		return "", err
	}
	return p.http.Complete(ctx, system, user)
}

// Ready reports whether the subprocess is currently up and healthy.
func (p *PythonCompleter) Ready(ctx context.Context) bool {
	p.mu.Lock()
	up := p.running
	p.mu.Unlock()
	return up && p.http.Healthy(ctx)
}

func (p *PythonCompleter) ensureUpLocked(ctx context.Context) error {
	if p.running && p.http.Healthy(ctx) {
		return nil
	}
	p.running = false

	cmd := exec.CommandContext(ctx, p.cfg.Python, p.cfg.Script, "--addr", p.cfg.Addr)
	cmd.Dir = p.cfg.Dir
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	threads := strconv.Itoa(p.threads())
	for _, k := range []string{"OMP_NUM_THREADS", "MKL_NUM_THREADS", "OPENBLAS_NUM_THREADS", "VECLIB_MAXIMUM_THREADS"} {
		cmd.Env = append(cmd.Env, k+"="+threads)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%w: %v", ErrCompleterNotReady, err)
	}
	log.Printf("llm: started %s %s (pid %d)", p.cfg.Python, p.cfg.Script, cmd.Process.Pid)
	go pipeLog("llm", stdout)
	go pipeLog("llm", stderr)

	p.cmd = cmd
	go func() {
		cmd.Wait()
		p.mu.Lock()
		if p.cmd == cmd {
			p.running = false
		}
		p.mu.Unlock()
	}()

	deadline := time.Now().Add(p.startTimeout())
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p.http.Healthy(ctx) {
			p.running = true
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("%w: model did not load within %s", ErrCompleterNotReady, p.startTimeout())
}

func (p *PythonCompleter) threads() int {
	if p.cfg.Threads > 0 {
		return p.cfg.Threads
	}
	return 4
}
