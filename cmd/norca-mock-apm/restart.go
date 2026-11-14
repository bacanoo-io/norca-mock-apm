package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bacanoo-io/norca-mock-apm/pkg/instance"
	"github.com/bacanoo-io/norca-mock-apm/pkg/server"
	"github.com/spf13/cobra"
)

var restartDaemonMode bool

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the mock APM server",
	RunE: func(cmd *cobra.Command, args []string) error {
		absDir, err := filepath.Abs(dataDir)
		if err != nil {
			return fmt.Errorf("invalid data dir: %w", err)
		}

		inst, err := instance.Read(absDir)
		if err == nil && inst != nil {
			fmt.Printf("Stopping existing NORCA Mock APM instance (PID: %d)...\n", inst.PID)
			callCtx, callCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = server.CallShutdown(callCtx, inst.AdminPort)
			callCancel()

			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if !instance.IsProcessAlive(inst.PID) {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}

			if instance.IsProcessAlive(inst.PID) {
				if proc, err := os.FindProcess(inst.PID); err == nil {
					_ = proc.Kill()
				}
			}
			_ = instance.Remove(absDir)
			time.Sleep(500 * time.Millisecond) // Allow ports to be fully released
		}

		fmt.Println("Starting NORCA Mock APM...")
		if restartDaemonMode {
			return runDaemon(absDir)
		}
		return runForeground(absDir)
	},
}

func init() {
	restartCmd.Flags().BoolVarP(&restartDaemonMode, "daemon", "b", true, "Run restarted server in the background (daemon mode)")
	rootCmd.AddCommand(restartCmd)
}

