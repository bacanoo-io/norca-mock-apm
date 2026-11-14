//go:build !windows

package instance

import (
	"os/exec"
	"syscall"
)

func SetDaemonProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

