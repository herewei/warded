package cmd

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/herewei/warded/internal/application"
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
		"--data-dir", t.TempDir(),
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
		"--data-dir", t.TempDir(),
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

func TestExplainActivateError_ForListenPortPermission(t *testing.T) {
	t.Parallel()

	err := explainActivateError(
		errors.Join(application.ErrListenPortPermission, syscall.EACCES),
		"",
		"",
		443,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "requires elevated privileges") {
		t.Fatalf("expected privilege guidance, got %q", msg)
	}
	if runtime.GOOS == "linux" {
		if !strings.Contains(msg, "CAP_NET_BIND_SERVICE") || !strings.Contains(msg, "setcap") {
			t.Fatalf("expected Linux setcap guidance, got %q", msg)
		}
	} else {
		if strings.Contains(msg, "setcap") {
			t.Fatalf("did not expect Linux-only setcap guidance on %s, got %q", runtime.GOOS, msg)
		}
	}
}

func TestExplainActivateError_ForListenPortOccupied(t *testing.T) {
	t.Parallel()

	err := explainActivateError(
		errors.Join(application.ErrListenPortOccupied, syscall.EADDRINUSE),
		"",
		"",
		443,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "port 443 is in use") {
		t.Fatalf("expected occupied guidance, got %q", msg)
	}
}
