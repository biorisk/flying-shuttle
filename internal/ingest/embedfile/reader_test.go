package embedfile_test

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/biorisk/flying-shuttle/internal/ingest/embedfile"
)

// writeFembed writes a minimal .fembed file for testing.
func writeFembed(t *testing.T, path string, dims int, records []embedfile.Record) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	// Header
	f.Write([]byte{0x46, 0x45, 0x4D, 0x42}) // magic "FEMB"
	writeU16(f, 1)                           // version
	writeU16(f, uint16(dims))               // dims
	writeU32(f, uint32(len(records)))       // record_count
	f.Write(make([]byte, 4))                 // reserved

	for _, rec := range records {
		sf := []byte(rec.SourceFile)
		writeU32(f, uint32(len(sf)))
		f.Write(sf)
		writeI32(f, rec.StartToken)
		txt := []byte(rec.Text)
		writeU32(f, uint32(len(txt)))
		f.Write(txt)
		for _, v := range rec.Embedding {
			var buf [4]byte
			binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
			f.Write(buf[:])
		}
	}
}

func writeU16(f *os.File, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	f.Write(b[:])
}

func writeU32(f *os.File, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	f.Write(b[:])
}

func writeI32(f *os.File, v int32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	f.Write(b[:])
}

func TestRoundTrip(t *testing.T) {
	want := []embedfile.Record{
		{
			SourceFile: "doc1.txt",
			StartToken: 0,
			Text:       "Hello world",
			Embedding:  []float32{0.1, 0.2, 0.3, 0.4},
		},
		{
			SourceFile: "doc2.txt",
			StartToken: 150,
			Text:       "Another chunk",
			Embedding:  []float32{0.5, 0.6, 0.7, 0.8},
		},
	}

	path := filepath.Join(t.TempDir(), "test.fembed")
	writeFembed(t, path, 4, want)

	r, err := embedfile.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	if r.Dims() != 4 {
		t.Errorf("Dims() = %d, want 4", r.Dims())
	}
	if r.Count() != 2 {
		t.Errorf("Count() = %d, want 2", r.Count())
	}

	for i, w := range want {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("record %d: Next() error: %v", i, err)
		}
		if got.SourceFile != w.SourceFile {
			t.Errorf("record %d: SourceFile = %q, want %q", i, got.SourceFile, w.SourceFile)
		}
		if got.StartToken != w.StartToken {
			t.Errorf("record %d: StartToken = %d, want %d", i, got.StartToken, w.StartToken)
		}
		if got.Text != w.Text {
			t.Errorf("record %d: Text = %q, want %q", i, got.Text, w.Text)
		}
		for j, v := range w.Embedding {
			if got.Embedding[j] != v {
				t.Errorf("record %d: Embedding[%d] = %v, want %v", i, j, got.Embedding[j], v)
			}
		}
	}

	// Next call should return EOF.
	_, err = r.Next()
	if err != io.EOF {
		t.Errorf("after all records: got %v, want io.EOF", err)
	}
}

func TestBadMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.fembed")
	f, _ := os.Create(path)
	f.Write(make([]byte, 16)) // all zeros, wrong magic
	f.Close()

	_, err := embedfile.Open(path)
	if err == nil {
		t.Fatal("expected error for bad magic bytes, got nil")
	}
}

// --- TSVReader tests ---

func writeTSV(t *testing.T, path string, records []embedfile.Record) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	fmt.Fprintf(f, "file_name\tstart_token\tembedding\ttext\n")
	for _, rec := range records {
		parts := make([]string, len(rec.Embedding))
		for i, v := range rec.Embedding {
			parts[i] = fmt.Sprintf("%v", v)
		}
		fmt.Fprintf(f, "%s\t%d\t%s\t%s\n",
			rec.SourceFile, rec.StartToken,
			strings.Join(parts, ","), rec.Text)
	}
}

func TestTSVRoundTrip(t *testing.T) {
	want := []embedfile.Record{
		{SourceFile: "a.txt", StartToken: 0, Text: "hello world", Embedding: []float32{0.1, 0.2, 0.3}},
		{SourceFile: "b.txt", StartToken: 150, Text: "another chunk", Embedding: []float32{0.4, 0.5, 0.6}},
	}

	path := filepath.Join(t.TempDir(), "test.embed")
	writeTSV(t, path, want)

	r, err := embedfile.OpenTSV(path)
	if err != nil {
		t.Fatalf("OpenTSV: %v", err)
	}
	defer r.Close()

	for i, w := range want {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		if got.SourceFile != w.SourceFile {
			t.Errorf("record %d: SourceFile = %q, want %q", i, got.SourceFile, w.SourceFile)
		}
		if got.StartToken != w.StartToken {
			t.Errorf("record %d: StartToken = %d, want %d", i, got.StartToken, w.StartToken)
		}
		if got.Text != w.Text {
			t.Errorf("record %d: Text = %q, want %q", i, got.Text, w.Text)
		}
		for j, v := range w.Embedding {
			if got.Embedding[j] != v {
				t.Errorf("record %d: Embedding[%d] = %v, want %v", i, j, got.Embedding[j], v)
			}
		}
	}

	if r.Dims() != 3 {
		t.Errorf("Dims() = %d, want 3", r.Dims())
	}

	_, err = r.Next()
	if err != io.EOF {
		t.Errorf("after all records: got %v, want io.EOF", err)
	}
}

func TestTSVBadHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.embed")
	f, _ := os.Create(path)
	fmt.Fprintln(f, "wrong\theader\there\tnope")
	f.Close()

	_, err := embedfile.OpenTSV(path)
	if err == nil {
		t.Fatal("expected error for bad header, got nil")
	}
}

func TestStreamerInterface(t *testing.T) {
	// Verify both readers satisfy the Streamer interface at compile time.
	want := []embedfile.Record{
		{SourceFile: "x.txt", StartToken: 0, Text: "test", Embedding: []float32{1, 2, 3, 4}},
	}

	dir := t.TempDir()

	// Binary
	binaryPath := filepath.Join(dir, "test.fembed")
	writeFembed(t, binaryPath, 4, want)
	var _ embedfile.Streamer
	br, _ := embedfile.Open(binaryPath)
	var bs embedfile.Streamer = br
	rec, _ := bs.Next()
	bs.Close()
	if rec.SourceFile != "x.txt" {
		t.Errorf("binary streamer: got %q", rec.SourceFile)
	}

	// TSV
	tsvPath := filepath.Join(dir, "test.embed")
	writeTSV(t, tsvPath, want)
	tr, _ := embedfile.OpenTSV(tsvPath)
	var ts embedfile.Streamer = tr
	rec, _ = ts.Next()
	ts.Close()
	if rec.SourceFile != "x.txt" {
		t.Errorf("tsv streamer: got %q", rec.SourceFile)
	}
}
