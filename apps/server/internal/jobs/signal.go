package jobs

import "context"

// Signal is a coalescing wake-up: any number of Wake calls before the receiver looks count as one. A worker
// waits on it next to its timer to start a pass early, and the code that produces work holds only the Wake
// method, so neither needs the other to exist first.
type Signal struct{ c chan struct{} }

// NewSignal returns a Signal nobody has woken yet.
func NewSignal() *Signal { return &Signal{c: make(chan struct{}, 1)} }

// Wake asks the receiver to run; it never blocks.
func (s *Signal) Wake() {
	select {
	case s.c <- struct{}{}:
	default:
	}
}

// C is the channel a woken receiver reads from.
func (s *Signal) C() <-chan struct{} { return s.c }

// Wait blocks until the signal is woken or ctx ends; it reports whether it was woken.
func (s *Signal) Wait(ctx context.Context) bool {
	select {
	case <-s.c:
		return true
	case <-ctx.Done():
		return false
	}
}
