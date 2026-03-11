package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	s := New(nil)
	if s == nil {
		t.Fatal("expected spinner to be created")
	}
	if s.interval != 200*time.Millisecond {
		t.Errorf("expected default interval 200ms, got %v", s.interval)
	}
}

func TestWithInterval(t *testing.T) {
	s := New(nil, WithInterval(500*time.Millisecond))
	if s.interval != 500*time.Millisecond {
		t.Errorf("expected interval 500ms, got %v", s.interval)
	}
}

func TestWithMessage(t *testing.T) {
	s := New(nil, WithMessage("Testing..."))
	if s.message != "Testing..." {
		t.Errorf("expected message 'Testing...', got %v", s.message)
	}
}

func TestSpinner_StartStop(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, WithInterval(50*time.Millisecond), WithMessage("Test"))

	s.Start()
	time.Sleep(150 * time.Millisecond)
	s.Stop()

	// Should have cleared the line
	output := buf.String()
	if strings.Contains(output, "Test") {
		// For non-TTY, it prints the message
		t.Logf("Output (non-TTY mode): %q", output)
	}
}

func TestSpinner_Succeed(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, WithInterval(50*time.Millisecond), WithMessage("Test"))

	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Succeed()

	output := buf.String()
	if !strings.Contains(output, "Test") {
		t.Errorf("expected output to contain 'Test', got %q", output)
	}
	if !strings.Contains(output, "✓") {
		t.Errorf("expected output to contain success symbol '✓', got %q", output)
	}
}

func TestSpinner_Fail(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, WithInterval(50*time.Millisecond), WithMessage("Test"))

	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Fail()

	output := buf.String()
	if !strings.Contains(output, "Test") {
		t.Errorf("expected output to contain 'Test', got %q", output)
	}
	if !strings.Contains(output, "✗") {
		t.Errorf("expected output to contain failure symbol '✗', got %q", output)
	}
}

func TestSpinner_MultipleStarts(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf, WithInterval(50*time.Millisecond))

	// Multiple starts should be safe
	s.Start()
	s.Start()
	s.Start()

	time.Sleep(100 * time.Millisecond)
	s.Stop()
}

func TestSpinner_StopWithoutStart(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)

	// Should not panic
	s.Stop()
	s.Succeed()
	s.Fail()
}

func TestFormatDuration(t *testing.T) {
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
			result := formatDuration(tt.input)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBrailleFrames(t *testing.T) {
	if len(brailleFrames) != 10 {
		t.Errorf("expected 10 braille frames, got %d", len(brailleFrames))
	}

	// Verify each frame is a single braille character
	for i, frame := range brailleFrames {
		if len([]rune(frame)) != 1 {
			t.Errorf("frame %d is not a single character: %q", i, frame)
		}
	}
}
