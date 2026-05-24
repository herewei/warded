package upstream

import (
	"os/exec"
	"syscall"
	"time"
)

func startWithProcessGroup(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}

func shutdownCmd(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		<-done
		return nil
	}
}
