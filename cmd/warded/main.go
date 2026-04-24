package main

import (
	"log/slog"
	"os"

	"github.com/herewei/warded/internal/cmd"
)

// Version is injected at build time via -ldflags "-X main.Version=x.y.z".
var Version = "v0.3.0"

// BuildDate is injected at build time via -ldflags.
var BuildDate = "unknown"

// GitCommit is injected at build time via -ldflags.
var GitCommit = "unknown"

// GoVersion is injected at build time via -ldflags.
var GoVersion = "unknown"

func main() {
	logLevel := new(slog.LevelVar)
	logLevel.Set(slog.LevelWarn) // quiet by default; --verbose raises to LevelDebug
	slog.SetDefault(slog.New(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}),
	))

	root := cmd.NewRootCommand(logLevel, cmd.BuildInfo{
		Version:   Version,
		BuildDate: BuildDate,
		GitCommit: GitCommit,
		GoVersion: GoVersion,
	})
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
