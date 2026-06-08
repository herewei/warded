package systemd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
)

type Host struct{}

func (Host) IsLinux() bool {
	return runtime.GOOS == "linux"
}

func (Host) IsRoot() bool {
	return os.Geteuid() == 0
}

func (Host) HasSystemctl() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func (Host) CurrentUsername() string {
	current, err := user.Current()
	if err != nil || current == nil || current.Username == "" {
		return os.Getenv("USER")
	}
	return current.Username
}

func (Host) HomeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

func (Host) ExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			return "", err
		}
	}
	return path, nil
}

func (Host) WriteUnit(_ context.Context, path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (Host) RunSystemctl(ctx context.Context, userScope bool, args ...string) error {
	cmdArgs := append([]string(nil), args...)
	if userScope {
		cmdArgs = append([]string{"--user"}, cmdArgs...)
	}
	cmd := exec.CommandContext(ctx, "systemctl", cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl %v: %w: %s", cmdArgs, err, string(out))
	}
	return nil
}
