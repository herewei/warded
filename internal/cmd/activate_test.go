package cmd

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestActivateCommandRejectsInvalidSite(t *testing.T) {
	t.Parallel()

	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, "test")
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{
		"activate",
		"--site", "foo",
		"--config-dir", t.TempDir(),
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported site: foo") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRootCommandSuppressesUsageForCommandErrors(t *testing.T) {
	t.Parallel()

	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, "test")
	root.SetOut(io.Discard)

	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"activate",
		"--site", "foo",
		"--config-dir", t.TempDir(),
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr.String(), "unsupported site: foo") {
		t.Fatalf("expected stderr to contain command error, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected usage to be suppressed, got %q", stderr.String())
	}
}
