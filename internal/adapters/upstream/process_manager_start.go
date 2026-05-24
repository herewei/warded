package upstream

import (
	"os/exec"
	"strings"
)

func startManagedProcess(command string) (*exec.Cmd, error) {
	trimmed := strings.TrimSpace(command)
	if argv, ok := splitNodeArgv(trimmed); ok {
		cmd := exec.Command(argv[0], argv[1:]...)
		if err := startWithProcessGroup(cmd); err != nil {
			return nil, err
		}
		return cmd, nil
	}
	cmd := exec.Command("sh", "-c", command)
	if err := startWithProcessGroup(cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}

// splitNodeArgv parses commands like: node '/path/to/script.js' --flag value
func splitNodeArgv(command string) ([]string, bool) {
	var args []string
	var cur strings.Builder
	inSingle := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\'' && !inSingle:
			inSingle = true
		case c == '\'' && inSingle:
			inSingle = false
		case c == ' ' && !inSingle:
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	if len(args) == 0 || args[0] != "node" {
		return nil, false
	}
	return args, true
}
