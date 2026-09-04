package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEmbedServer records the peak number of concurrent /embed requests.
func fakeEmbedServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var inFlight, peak int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)

		var req struct{ Texts []string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		out := make([][]float32, len(req.Texts))
		for i := range out {
			out[i] = []float32{1, 0, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": out, "dim": 3})
	}))
	t.Cleanup(srv.Close)
	return srv, &peak
}

func TestPythonEmbedder_SingleFlight(t *testing.T) {
	srv, peak := fakeEmbedServer(t)
	p := &PythonEmbedder{http: NewHTTPEmbedder(srv.URL), Gate: NewComputeGate()}
	p.ready.Store(true)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.EmbedBatch(context.Background(), []string{"x"}); err != nil {
				t.Errorf("EmbedBatch: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(peak); got != 1 {
		t.Fatalf("expected single-flight (peak concurrency 1), got %d", got)
	}
}

func TestPythonEmbedder_NotReady(t *testing.T) {
	p := &PythonEmbedder{http: NewHTTPEmbedder("http://127.0.0.1:0"), Gate: NewComputeGate()}
	if _, err := p.EmbedBatch(context.Background(), []string{"x"}); err != ErrEmbedderNotReady {
		t.Fatalf("want ErrEmbedderNotReady, got %v", err)
	}
}

func TestPythonEmbedder_ThreadAndGraceDefaults(t *testing.T) {
	p := &PythonEmbedder{}
	if p.threads() != defaultEmbedThreads {
		t.Fatalf("threads default = %d", p.threads())
	}
	if p.termGrace() != defaultTermGrace {
		t.Fatalf("term grace default = %v", p.termGrace())
	}
	p.cfg.Threads, p.cfg.TermGrace = 2, time.Second
	if p.threads() != 2 || p.termGrace() != time.Second {
		t.Fatal("config overrides not honoured")
	}
}
