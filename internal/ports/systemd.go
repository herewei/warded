package ports

import "context"

type SystemdHost interface {
	IsLinux() bool
	IsRoot() bool
	HasSystemctl() bool
	CurrentUsername() string
	HomeDir() string
	ExecutablePath() (string, error)
	WriteUnit(ctx context.Context, path string, content string) error
	RunSystemctl(ctx context.Context, userScope bool, args ...string) error
}
