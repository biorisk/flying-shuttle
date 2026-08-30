package ingest

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// PythonEmbedderConfig configures a managed local embedding server.
type PythonEmbedderConfig struct {
	Python string // interpreter, e.g. "python3" or "python/.venv/bin/python"
	Script string // path to embed_server.py
	Addr   string // host:port the server listens on, e.g. "127.0.0.1:8071"
	Dir    string // working directory for the child (where the model lives)
}

// PythonEmbedder spawns and supervises python/embed_server.py and proxies
// embedding calls to it over HTTP. It implements Embedder.
//
// The process is restarted with capped backoff if it exits. Calls made before
// the model has loaded return ErrEmbedderNotReady, so ingestion can proceed
// and backfill later.
type PythonEmbedder struct {
	cfg  PythonEmbedderConfig
	http *HTTPEmbedder

	ready  atomic.Bool
	gaveUp atomic.Bool

	mu  sync.Mutex
	cmd *exec.Cmd
}

// NewPythonEmbedder creates a supervisor. Call Start to launch the process.
func NewPythonEmbedder(cfg PythonEmbedderConfig) *PythonEmbedder {
	return &PythonEmbedder{
		cfg:  cfg,
		http: NewHTTPEmbedder("http://" + cfg.Addr),
	}
}

// Ready reports whether the backend is up and the model is loaded.
func (p *PythonEmbedder) Ready() bool { return p.ready.Load() }

// Start launches the supervision loop. It returns immediately; the process
// comes up asynchronously. The loop exits when ctx is cancelled.
func (p *PythonEmbedder) Start(ctx context.Context) {
	if _, err := os.Stat(p.cfg.Script); err != nil {
		log.Printf("embedder: %s not found, embeddings disabled (%v)", p.cfg.Script, err)
		return
	}
	go p.supervise(ctx)
	go p.pollHealth(ctx)
}

func (p *PythonEmbedder) supervise(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	const maxFastFailures = 5 // consecutive quick exits before giving up

	fastFailures := 0
	for {
		if ctx.Err() != nil {
			return
		}

		start := time.Now()
		err := p.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		p.ready.Store(false)
		ranFor := time.Since(start)

		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("embedder: process exited after %s: %v", ranFor.Round(time.Millisecond), err)
		} else {
			log.Printf("embedder: process exited after %s", ranFor.Round(time.Millisecond))
		}

		if ranFor > time.Minute {
			backoff = time.Second
			fastFailures = 0
		} else {
			fastFailures++
		}
		if fastFailures >= maxFastFailures {
			log.Printf("embedder: giving up after %d failed starts; vector search disabled. "+
				"Fix the embedder and restart shuttle.", fastFailures)
			p.gaveUp.Store(true)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// runOnce starts the child and blocks until it exits or ctx is cancelled.
func (p *PythonEmbedder) runOnce(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, p.cfg.Python, p.cfg.Script, "--addr", p.cfg.Addr)
	cmd.Dir = p.cfg.Dir
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("embedder: started %s %s (pid %d)", p.cfg.Python, p.cfg.Script, cmd.Process.Pid)

	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()

	go pipeLog("embedder", stdout)
	go pipeLog("embedder", stderr)

	return cmd.Wait()
}

func (p *PythonEmbedder) pollHealth(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if p.gaveUp.Load() {
				return
			}
			healthy := p.http.Healthy(ctx)
			if healthy && !p.ready.Swap(true) {
				log.Printf("embedder: ready (%s)", p.cfg.Addr)
			} else if !healthy && p.ready.Swap(false) {
				log.Printf("embedder: unhealthy (%s)", p.cfg.Addr)
			}
		}
	}
}

func pipeLog(prefix string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		log.Printf("%s: %s", prefix, sc.Text())
	}
}

// Embed implements Embedder.
func (p *PythonEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if !p.ready.Load() {
		return nil, ErrEmbedderNotReady
	}
	return p.http.Embed(ctx, text)
}

// EmbedBatch implements Embedder.
func (p *PythonEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if !p.ready.Load() {
		return nil, ErrEmbedderNotReady
	}
	return p.http.EmbedBatch(ctx, texts)
}
