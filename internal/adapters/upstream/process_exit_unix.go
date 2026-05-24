package upstream

import (
	"os/exec"
	"syscall"
)

func managedProcessExited(cmd *exec.Cmd) (bool, error) {
	if cmd == nil || cmd.Process == nil {
		return false, nil
	}
	var status syscall.WaitStatus
	pid, err := syscall.Wait4(cmd.Process.Pid, &status, syscall.WNOHANG, nil)
	if err != nil {
		return false, err
	}
	return pid > 0, nil
}
