package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

var brailleFrames = []string{
	"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
}

// Spinner provides a terminal loading animation
type Spinner struct {
	w          io.Writer
	mu         sync.Mutex
	running    bool
	stopCh     chan struct{}
	message    string
	interval   time.Duration
	startTime  time.Time
	frameIndex int
	isTTY      bool
}

// Option configures the Spinner
type Option func(*Spinner)

// WithInterval sets the animation interval (default 200ms)
func WithInterval(d time.Duration) Option {
	return func(s *Spinner) {
		s.interval = d
	}
}

// WithMessage sets the spinner message prefix
func WithMessage(msg string) Option {
	return func(s *Spinner) {
		s.message = msg
	}
}

// New creates a new Spinner writing to the given writer
func New(w io.Writer, opts ...Option) *Spinner {
	if w == nil {
		w = os.Stdout
	}

	s := &Spinner{
		w:        w,
		interval: 200 * time.Millisecond,
		message:  "Loading...",
		isTTY:    isTerminal(w),
		stopCh:   make(chan struct{}),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Start begins the spinner animation
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	s.running = true
	s.startTime = time.Now()
	s.frameIndex = 0

	// For non-TTY, just print the message once
	if !s.isTTY {
		fmt.Fprintf(s.w, "%s\n", s.message)
		return
	}

	go s.run()
}

// Stop halts the spinner animation
func (s *Spinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.running = false
	close(s.stopCh)

	// Clear the line
	if s.isTTY {
		fmt.Fprint(s.w, "\r\033[K")
	}
}

// Succeed stops the spinner and shows a success indicator
func (s *Spinner) Succeed() {
	s.stopWithSymbol("✓")
}

// Fail stops the spinner and shows a failure indicator
func (s *Spinner) Fail() {
	s.stopWithSymbol("✗")
}

func (s *Spinner) stopWithSymbol(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.running = false
	close(s.stopCh)

	elapsed := time.Since(s.startTime).Round(time.Second)

	if s.isTTY {
		fmt.Fprintf(s.w, "\r%s %s %s\n", s.message, symbol, formatDuration(elapsed))
	} else {
		fmt.Fprintf(s.w, "%s %s %s\n", s.message, symbol, formatDuration(elapsed))
	}
}

func (s *Spinner) run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.render()
		}
	}
}

func (s *Spinner) render() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	elapsed := time.Since(s.startTime).Round(time.Second)
	frame := brailleFrames[s.frameIndex%len(brailleFrames)]
	s.frameIndex++

	fmt.Fprintf(s.w, "\r%s %s %s", s.message, frame, formatDuration(elapsed))
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// isTerminal checks if the writer is a terminal
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		info, err := f.Stat()
		if err != nil {
			return false
		}
		// Check if it's a character device (terminal)
		return info.Mode()&os.ModeCharDevice != 0
	}
	return false
}
