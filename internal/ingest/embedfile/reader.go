// Package embedfile reads .fembed binary embedding files produced by python/embed.py.
//
// File format (.fembed):
//
//	Header (16 bytes):
//	  [4]  magic: 0x46 0x45 0x4D 0x42 ("FEMB")
//	  [2]  version: uint16 LE = 1
//	  [2]  dims: uint16 LE
//	  [4]  record_count: uint32 LE
//	  [4]  reserved: 0x00*4
//
//	Per record (variable length):
//	  [4]  source_file_len: uint32 LE
//	  [N]  source_file: UTF-8 bytes
//	  [4]  start_token: int32 LE
//	  [4]  text_len: uint32 LE
//	  [M]  text: UTF-8 bytes
//	  [dims*4]  embedding: float32 LE array (IEEE 754)
package embedfile

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

var magic = [4]byte{0x46, 0x45, 0x4D, 0x42} // "FEMB"

// Record holds a single embedding record from a .fembed file.
type Record struct {
	SourceFile string
	StartToken int32
	Text       string
	Embedding  []float32
}

// Reader streams records from a .fembed binary file.
type Reader struct {
	f     *os.File
	dims  uint16
	count uint32
	read  uint32
}

// Open opens a .fembed file and validates its header.
// Call Close when done.
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var hdr [16]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		f.Close()
		return nil, fmt.Errorf("read header: %w", err)
	}

	if hdr[0] != magic[0] || hdr[1] != magic[1] || hdr[2] != magic[2] || hdr[3] != magic[3] {
		f.Close()
		return nil, fmt.Errorf("invalid magic bytes: not a .fembed file")
	}

	version := binary.LittleEndian.Uint16(hdr[4:6])
	if version != 1 {
		f.Close()
		return nil, fmt.Errorf("unsupported .fembed version %d (want 1)", version)
	}

	dims := binary.LittleEndian.Uint16(hdr[6:8])
	count := binary.LittleEndian.Uint32(hdr[8:12])

	return &Reader{f: f, dims: dims, count: count}, nil
}

// Close closes the underlying file.
func (r *Reader) Close() error { return r.f.Close() }

// Dims returns the embedding dimension declared in the file header.
func (r *Reader) Dims() int { return int(r.dims) }

// Count returns the total number of records declared in the file header.
func (r *Reader) Count() int { return int(r.count) }

// Next reads and returns the next record. Returns io.EOF when all records
// have been read, or a wrapped error if the file is corrupted.
func (r *Reader) Next() (*Record, error) {
	if r.read >= r.count {
		return nil, io.EOF
	}

	var sfLen uint32
	if err := binary.Read(r.f, binary.LittleEndian, &sfLen); err != nil {
		return nil, fmt.Errorf("read source_file_len at record %d: %w", r.read, err)
	}

	sfBytes := make([]byte, sfLen)
	if _, err := io.ReadFull(r.f, sfBytes); err != nil {
		return nil, fmt.Errorf("read source_file at record %d: %w", r.read, err)
	}

	var startToken int32
	if err := binary.Read(r.f, binary.LittleEndian, &startToken); err != nil {
		return nil, fmt.Errorf("read start_token at record %d: %w", r.read, err)
	}

	var textLen uint32
	if err := binary.Read(r.f, binary.LittleEndian, &textLen); err != nil {
		return nil, fmt.Errorf("read text_len at record %d: %w", r.read, err)
	}

	textBytes := make([]byte, textLen)
	if _, err := io.ReadFull(r.f, textBytes); err != nil {
		return nil, fmt.Errorf("read text at record %d: %w", r.read, err)
	}

	vec := make([]float32, r.dims)
	if err := binary.Read(r.f, binary.LittleEndian, vec); err != nil {
		return nil, fmt.Errorf("read embedding at record %d: %w", r.read, err)
	}

	r.read++
	return &Record{
		SourceFile: string(sfBytes),
		StartToken: startToken,
		Text:       string(textBytes),
		Embedding:  vec,
	}, nil
}
