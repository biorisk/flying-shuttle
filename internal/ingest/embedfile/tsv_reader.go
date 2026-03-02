package embedfile

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

var tsvHeader = []string{"file_name", "start_token", "embedding", "text"}

// TSVReader streams records from a legacy .embed TSV file produced by the
// original python/embed.py output format. It returns the same Record type
// as the binary Reader so both can be used interchangeably via Streamer.
//
// Column layout (tab-separated, with header row):
//
//	file_name  start_token  embedding  text
//
// where embedding is a comma-separated list of float32 values.
type TSVReader struct {
	f    *os.File
	csv  *csv.Reader
	dims int
}

// OpenTSV opens a .embed TSV file and validates its header row.
// Call Close when done.
func OpenTSV(path string) (*TSVReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	cr := csv.NewReader(f)
	cr.Comma = '\t'
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1 // rows may have trailing whitespace; validate manually

	header, err := cr.Read()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("read header: %w", err)
	}
	if len(header) < 4 ||
		header[0] != tsvHeader[0] || header[1] != tsvHeader[1] ||
		header[2] != tsvHeader[2] || header[3] != tsvHeader[3] {
		f.Close()
		return nil, fmt.Errorf("invalid .embed header %v, expected %v", header, tsvHeader)
	}

	return &TSVReader{f: f, csv: cr}, nil
}

// Close closes the underlying file.
func (r *TSVReader) Close() error { return r.f.Close() }

// Dims returns the embedding dimension detected from the first record read,
// or 0 if no records have been read yet.
func (r *TSVReader) Dims() int { return r.dims }

// Next reads and returns the next record. Returns io.EOF when exhausted.
func (r *TSVReader) Next() (*Record, error) {
	for {
		row, err := r.csv.Read()
		if err == io.EOF {
			return nil, io.EOF
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		if len(row) < 4 {
			// Skip malformed rows rather than aborting.
			continue
		}

		startToken, err := strconv.ParseInt(strings.TrimSpace(row[1]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("parse start_token %q: %w", row[1], err)
		}

		parts := strings.Split(row[2], ",")
		embedding := make([]float32, len(parts))
		for i, s := range parts {
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
			if err != nil {
				return nil, fmt.Errorf("parse embedding[%d] %q: %w", i, s, err)
			}
			embedding[i] = float32(v)
		}

		if r.dims == 0 {
			r.dims = len(embedding)
		}

		return &Record{
			SourceFile: row[0],
			StartToken: int32(startToken),
			Text:       row[3],
			Embedding:  embedding,
		}, nil
	}
}
