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

var shutdownCmd = &cobra.Command{
	Use:   "shutdown",
	Short: "Shut down the running mock APM server",
	RunE: func(cmd *cobra.Command, args []string) error {
		absDir, err := filepath.Abs(dataDir)
		if err != nil {
			return fmt.Errorf("invalid data dir: %w", err)
		}

		inst, err := instance.Read(absDir)
		if err != nil {
			fmt.Println("No running NORCA Mock APM instance found.")
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, statusErr := server.GetStatus(ctx, inst.AdminPort)
		cancel()

		if statusErr != nil && !instance.IsProcessAlive(inst.PID) {
			_ = instance.Remove(absDir)
			fmt.Println("No running NORCA Mock APM instance found (cleaned up stale pid file).")
			return nil
		}

		fmt.Printf("Sending shutdown signal to NORCA Mock APM (PID: %d, Admin Port: %d)...\n", inst.PID, inst.AdminPort)
		callCtx, callCancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = server.CallShutdown(callCtx, inst.AdminPort)
		callCancel()

		if err != nil {
			fmt.Printf("Could not contact admin API: %v. Attempting to kill process directly...\n", err)
			if proc, procErr := os.FindProcess(inst.PID); procErr == nil {
				_ = proc.Kill()
			}
		}

		// Wait for process to exit
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !instance.IsProcessAlive(inst.PID) {
				_ = instance.Remove(absDir)
				fmt.Printf("NORCA Mock APM (PID: %d) shut down successfully.\n", inst.PID)
				return nil
			}
			time.Sleep(200 * time.Millisecond)
		}

		// If still alive after 5s, kill it
		if proc, procErr := os.FindProcess(inst.PID); procErr == nil {
			_ = proc.Kill()
		}
		_ = instance.Remove(absDir)
		fmt.Printf("NORCA Mock APM (PID: %d) terminated.\n", inst.PID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(shutdownCmd)
}

