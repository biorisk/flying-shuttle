package ingest

import "context"

// ComputeGate is a 1-slot semaphore that serialises heavy local-model work
// across subsystems. On an 8GB M1 the embedder and the instruct LLM must not
// run large batches at the same time — both acquire the same ComputeGate so a
// digest pass and an embed backfill take turns rather than thrash memory.
//
// The zero value is not usable; call NewComputeGate. A nil *ComputeGate is a
// valid no-op (Acquire/Release do nothing), so callers without a shared gate
// wired in still work.
type ComputeGate struct {
	slot chan struct{}
}

// NewComputeGate returns an unlocked gate.
func NewComputeGate() *ComputeGate {
	return &ComputeGate{slot: make(chan struct{}, 1)}
}

// Acquire blocks until the gate is free or ctx is done. It returns ctx.Err()
// on cancellation, nil on success. A nil gate returns nil immediately.
func (g *ComputeGate) Acquire(ctx context.Context) error {
	if g == nil {
		return nil
	}
	select {
	case g.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees the gate. It must be called exactly once per successful
// Acquire. A nil gate is a no-op.
func (g *ComputeGate) Release() {
	if g == nil {
		return
	}
	select {
	case <-g.slot:
	default:
	}
}
