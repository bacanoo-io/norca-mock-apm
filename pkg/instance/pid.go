package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const PidFileName = "norca.pid"

type Info struct {
	PID       int       `json:"pid"`
	GRPCPort  int       `json:"grpc_port"`
	AdminPort int       `json:"admin_port"`
	DataDir   string    `json:"data_dir"`
	StartedAt time.Time `json:"started_at"`
}

func PidFilePath(dataDir string) string {
	return filepath.Join(dataDir, PidFileName)
}

func Write(dataDir string, info Info) error {
	p := PidFilePath(dataDir)
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal instance info: %w", err)
	}

	if err := os.WriteFile(p, data, 0644); err != nil {
		return fmt.Errorf("failed to write pid file: %w", err)
	}

	return nil
}

func Read(dataDir string) (*Info, error) {
	p := PidFilePath(dataDir)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("no instance pid file found")
		}
		return nil, fmt.Errorf("failed to read pid file: %w", err)
	}

	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pid file: %w", err)
	}

	return &info, nil
}

func Remove(dataDir string) error {
	p := PidFilePath(dataDir)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove pid file: %w", err)
	}
	return nil
}

// IsProcessAlive checks if the given pid is currently running.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows, FindProcess always succeeds, so we rely on signal or admin ping.
	// We'll provide a platform-aware or signal check where possible.
	return checkProcessAlive(proc)
}

