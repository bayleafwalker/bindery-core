package relayv1

import "sync"

// ReplayWindow accepts each sequence once while allowing limited reordering.
// It is intentionally small and allocation-local so forwarding does not need
// a durable dependency.
type ReplayWindow struct {
	mu          sync.Mutex
	initialized bool
	maximum     uint64
	seen        uint64
}

func (w *ReplayWindow) Accept(sequence uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.initialized {
		w.initialized = true
		w.maximum = sequence
		w.seen = 1
		return true
	}
	if sequence > w.maximum {
		shift := sequence - w.maximum
		if shift >= 64 {
			w.seen = 1
		} else {
			w.seen = w.seen<<shift | 1
		}
		w.maximum = sequence
		return true
	}
	distance := w.maximum - sequence
	if distance >= 64 {
		return false
	}
	bit := uint64(1) << distance
	if w.seen&bit != 0 {
		return false
	}
	w.seen |= bit
	return true
}
