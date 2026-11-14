//go:build !windows

package instance

import (
	"os"
	"syscall"
)

func checkProcessAlive(p *os.Process) bool {
	err := p.Signal(syscall.Signal(0))
	return err == nil
}

