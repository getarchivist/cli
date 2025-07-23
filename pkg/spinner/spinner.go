package spinner

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Spinner provides a simple terminal spinner for loading indicators
type Spinner struct {
	frames []string
	pos    int
	mu     sync.Mutex
	stop   chan bool
	done   chan bool
}

// New creates a new spinner with default frames
func New() *Spinner {
	return &Spinner{
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		pos:    0,
		stop:   make(chan bool),
		done:   make(chan bool),
	}
}

// Start begins the spinner animation with the given message
func (s *Spinner) Start(message string) {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				s.done <- true
				return
			case <-ticker.C:
				s.mu.Lock()
				s.pos = (s.pos + 1) % len(s.frames)
				frame := s.frames[s.pos]
				s.mu.Unlock()

				fmt.Fprintf(os.Stderr, "\r[ohsh] %s %s", frame, message)
			}
		}
	}()
}

// Stop stops the spinner and clears the line
func (s *Spinner) Stop() {
	s.stop <- true
	<-s.done
	fmt.Fprintf(os.Stderr, "\r\033[K") // Clear the line
}

// UpdateMessage updates the spinner message
func (s *Spinner) UpdateMessage(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(os.Stderr, "\r[ohsh] %s %s", s.frames[s.pos], message)
}
