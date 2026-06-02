package cmd

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runCmd executes a CLI command for testing purposes.
// root is the root cobra command.
// globalFlags are flags that should be set before the command (e.g., --format json).
// commandName is the command to run (e.g., "whitelist").
// args are the command arguments.
func runCmd(t *testing.T, root *cobra.Command, globalFlags []string, commandName string, args []string) (string, error) {
	t.Helper()

	// Find the command by name
	var cmd *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == commandName {
			cmd = c
			break
		}
	}
	if cmd == nil {
		t.Fatalf("command %q not found", commandName)
	}

	// Set global flags
	if len(globalFlags) > 0 {
		root.SetArgs(globalFlags)
		if err := root.ParseFlags(globalFlags); err != nil {
			return "", err
		}
	}

	// Set command arguments
	fullArgs := append([]string{commandName}, args...)
	root.SetArgs(fullArgs)

	// Capture output
	var output strings.Builder
	root.SetOut(&output)
	root.SetErr(&output)

	// Execute
	err := root.Execute()
	return output.String(), err
}

// parseJSONOutput parses JSON output from a command into a map[string]any.
func parseJSONOutput(t *testing.T, output string) map[string]any {
	t.Helper()

	var m map[string]any
	if err := json.Unmarshal([]byte(output), &m); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, output)
	}
	return m
}

// newTestRootCommand creates a root command for testing purposes.
func newTestRootCommand() *cobra.Command {
	logLevel := new(slog.LevelVar)
	info := BuildInfo{
		Version:   "test",
		BuildDate: "test",
		GitCommit: "test",
		GoVersion: "test",
	}
	return NewRootCommand(logLevel, info)
}
