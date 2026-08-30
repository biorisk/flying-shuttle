package api_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/biorisk/flying-shuttle/internal/api"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
)

// TestFullLoop drives the whole product loop through the real router — using
// only the "/" fragment endpoints (the JSON API is now ingest-only) and the
// store for setup/inspection: upload a transcript, write a bullet, pull
// evidence, open the transcript reader, attach a passage as a locked
// sub-bullet, preview the stitch, download the markdown.
func TestFullLoop(t *testing.T) {
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	idx := search.NewHybridIndex(nil) // BM25-only
	dir := t.TempDir()
	srv := httptest.NewServer(api.NewRouter(s, dir, nil, idx, &stitch.StubStitcher{}, func() {}))
	t.Cleanup(srv.Close)

	get := func(path string) string {
		t.Helper()
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("GET %s = %d: %s", path, res.StatusCode, b)
		}
		return string(b)
	}
	send := func(method, path string, v url.Values) string {
		t.Helper()
		var body io.Reader
		if v != nil {
			body = strings.NewReader(v.Encode())
		}
		req, _ := http.NewRequest(method, srv.URL+path, body)
		if v != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode/100 != 2 {
			t.Fatalf("%s %s = %d: %s", method, path, res.StatusCode, b)
		}
		return string(b)
	}
	form := func(path string, v url.Values) string { return send(http.MethodPost, path, v) }

	// 1. the shell renders
	shell := get("/")
	if !strings.Contains(shell, `id="shell"`) || !strings.Contains(shell, "datastar-v1.0.3") {
		t.Fatal("shell missing key markup")
	}

	// 2. upload a transcript through the ingest drawer endpoint
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("files", "interview.txt")
	fw.Write([]byte("I was afraid before the vote that morning.\n\nBut once I began speaking the fear turned into resolve and I carried the room."))
	mw.Close()
	res, err := http.Post(srv.URL+"/ingest", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	// wait for async chunking
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cs, _ := s.ListChunks(); len(cs) > 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	chunks, _ := s.ListChunks()
	if len(chunks) == 0 {
		t.Fatal("upload produced no chunks")
	}

	// 3. add a bullet and title it
	form("/outline/roots", nil)
	bulletID := onlyRoot(t, s)
	send(http.MethodPatch, "/outline/nodes/"+bulletID, url.Values{"title": {"the fear before the vote"}, "version": {"1"}})

	// 4. evidence pane finds the fear passages
	ev := get("/evidence?node=" + bulletID + "&q=" + url.QueryEscape("fear before the vote"))
	if !strings.Contains(ev, "candidate") || !strings.Contains(ev, "fear") {
		t.Fatalf("evidence pane found nothing:\n%s", ev)
	}
	chunkID := firstMatch(t, ev, `data-chunk="([^"]+)"`)

	// 5. open the transcript reader for that chunk
	rd := get("/evidence/transcript?node=" + bulletID + "&chunk=" + chunkID)
	if !strings.Contains(rd, `id="transcript-reader"`) || !strings.Contains(rd, "readerChunk") {
		t.Fatalf("transcript reader fragment wrong:\n%s", rd)
	}

	// 6. attach the whole chunk as evidence -> locked sub-bullet
	out := form("/outline/nodes/"+bulletID+"/evidence", url.Values{"chunk_id": {chunkID}})
	if !strings.Contains(out, `id="outline"`) || !strings.Contains(out, "bullet-evidence") {
		t.Fatalf("attach didn't produce an evidence bullet:\n%s", out)
	}
	kids := childrenOf(t, s, bulletID)
	if len(kids) != 1 {
		t.Fatalf("want 1 evidence sub-bullet, got %d", len(kids))
	}
	n, _ := s.GetNode(kids[0])
	if !n.Locked || n.Type != model.NodeTypeChunkRef {
		t.Fatalf("evidence bullet should be a locked chunk_ref: %+v", n)
	}

	// 7. preview stitches the attached passage
	st := get("/stitch?glue=50")
	if !strings.Contains(st, "span-chunk") || !strings.Contains(st, "% glue") {
		t.Fatalf("stitch preview wrong:\n%s", st)
	}

	// 8. markdown export streams a file
	res, err = http.Get(srv.URL + "/export.md?glue=50")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("export content-type = %q", ct)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "the fear turned into resolve") {
		t.Fatalf("exported markdown missing the passage:\n%s", body)
	}
}

func firstMatch(t *testing.T, s, pat string) string {
	t.Helper()
	m := regexp.MustCompile(pat).FindStringSubmatch(s)
	if len(m) < 2 {
		t.Fatalf("pattern %q not found in:\n%s", pat, s)
	}
	return m[1]
}

func onlyRoot(t *testing.T, s store.Store) string {
	t.Helper()
	nodes, _ := s.ListNodes()
	edges, _ := s.ListEdges()
	child := map[string]bool{}
	for _, e := range edges {
		if e.Type == model.EdgeTypeLinear {
			child[e.ToNode] = true
		}
	}
	var roots []string
	for _, n := range nodes {
		if n.Type == model.NodeTypeOutline && !child[n.ID] {
			roots = append(roots, n.ID)
		}
	}
	if len(roots) != 1 {
		t.Fatalf("want exactly 1 root bullet, got %d", len(roots))
	}
	return roots[0]
}

func childrenOf(t *testing.T, s store.Store, id string) []string {
	t.Helper()
	edges, _ := s.ListEdgesFrom(id)
	var out []string
	for _, e := range edges {
		if e.Type == model.EdgeTypeLinear {
			out = append(out, e.ToNode)
		}
	}
	return out
}
