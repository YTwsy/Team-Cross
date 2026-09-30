package lifecycle

import "sync"

// Startup defers explicit quit until ownership and Core startup have settled.
// Cancelling a context alone is insufficient: a detached helper may already
// have started, and must be discovered before the owner can stop it safely.
type Startup struct {
	mu      sync.Mutex
	done    bool
	pending bool
	Quit    func()
}

func (s *Startup) RequestQuit() {
	s.mu.Lock()
	if !s.done {
		s.pending = true
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.Quit()
}

func (s *Startup) Quitting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

// Finish must run after any Ensure call returns, on success and failure alike.
func (s *Startup) Finish() {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	pending := s.pending
	s.pending = false
	s.mu.Unlock()
	if pending {
		s.Quit()
	}
}
