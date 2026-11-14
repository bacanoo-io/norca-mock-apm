package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/bacanoo-io/norca-mock-apm/pkg/instance"
	"github.com/bacanoo-io/norca-mock-apm/pkg/server"
	"github.com/bacanoo-io/norca-mock-apm/pkg/storage"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear all collected telemetry data (logs, metrics, traces)",
	RunE: func(cmd *cobra.Command, args []string) error {
		absDir, err := filepath.Abs(dataDir)
		if err != nil {
			return fmt.Errorf("invalid data dir: %w", err)
		}

		inst, err := instance.Read(absDir)
		if err == nil && inst != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			resetErr := server.CallReset(ctx, inst.AdminPort)
			cancel()

			if resetErr == nil {
				fmt.Printf("Telemetry data reset successfully on running NORCA Mock APM instance (PID: %d).\n", inst.PID)
				return nil
			}
		}

		// Fallback: reset storage directly on disk
		stor, err := storage.NewStorage(absDir, includeMeta)
		if err != nil {
			return fmt.Errorf("failed to open storage for reset: %w", err)
		}

		if err := stor.Reset(); err != nil {
			return fmt.Errorf("failed to reset storage on disk: %w", err)
		}

		fmt.Printf("Telemetry data reset successfully in data directory: %s\n", absDir)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(resetCmd)
}

