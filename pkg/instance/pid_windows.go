//go:build windows

package instance

import (
	"os"

	"golang.org/x/sys/windows"
)

func checkProcessAlive(p *os.Process) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	err = windows.GetExitCodeProcess(h, &exitCode)
	if err != nil {
		return false
	}

	const stillActive = 259
	return exitCode == stillActive
}

