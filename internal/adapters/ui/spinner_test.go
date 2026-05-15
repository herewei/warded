package ui

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Parallel()

	s := New(nil)
	if s == nil {
		t.Fatal("expected spinner to be created")
	}
	if s.interval != 200*time.Millisecond {
		t.Errorf("expected default interval 200ms, got %v", s.interval)
	}
}

func TestWithInterval(t *testing.T) {
	t.Parallel()

	s := New(nil, WithInterval(500*time.Millisecond))
	if s.interval != 500*time.Millisecond {
		t.Errorf("expected interval 500ms, got %v", s.interval)
	}
}

func TestWithMessage(t *testing.T) {
	t.Parallel()

	s := New(nil, WithMessage("Testing..."))
	if s.message != "Testing..." {
		t.Errorf("expected message 'Testing...', got %v", s.message)
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    time.Duration
		expected string
	}{
		{5 * time.Second, "5s"},
		{30 * time.Second, "30s"},
		{60 * time.Second, "1m0s"},
		{90 * time.Second, "1m30s"},
		{125 * time.Second, "2m5s"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			result := formatDuration(tt.input)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBrailleFrames(t *testing.T) {
	t.Parallel()

	if len(brailleFrames) != 10 {
		t.Errorf("expected 10 braille frames, got %d", len(brailleFrames))
	}

	for i, frame := range brailleFrames {
		if len([]rune(frame)) != 1 {
			t.Errorf("frame %d is not a single character: %q", i, frame)
		}
	}
}
