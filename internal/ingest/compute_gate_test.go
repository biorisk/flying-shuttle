package ingest

import (
	"context"
	"testing"
	"time"
)

func TestComputeGate_Serialises(t *testing.T) {
	g := NewComputeGate()
	ctx := context.Background()

	if err := g.Acquire(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	got := make(chan error, 1)
	go func() { got <- g.Acquire(ctx) }()

	select {
	case <-got:
		t.Fatal("second acquire succeeded while gate held")
	case <-time.After(20 * time.Millisecond):
	}

	g.Release()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("second acquire after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second acquire did not unblock after release")
	}
	g.Release()
}

func TestComputeGate_ContextCancel(t *testing.T) {
	g := NewComputeGate()
	_ = g.Acquire(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()

	if err := g.Acquire(ctx); err != context.Canceled {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	g.Release()
}

func TestComputeGate_NilIsNoop(t *testing.T) {
	var g *ComputeGate
	if err := g.Acquire(context.Background()); err != nil {
		t.Fatalf("nil gate acquire: %v", err)
	}
	g.Release() // must not panic
}

func TestComputeGate_ExtraReleaseSafe(t *testing.T) {
	g := NewComputeGate()
	g.Release() // release without acquire — must not block or panic
	if err := g.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire after spurious release: %v", err)
	}
	g.Release()
}
