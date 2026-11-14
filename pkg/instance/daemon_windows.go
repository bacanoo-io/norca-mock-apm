//go:build windows

package instance

import (
	"os/exec"
	"syscall"
)

func SetDaemonProcAttr(cmd *exec.Cmd) {
	const (
		CREATE_NEW_PROCESS_GROUP = 0x00000200
		DETACHED_PROCESS         = 0x00000008
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS,
	}
}

