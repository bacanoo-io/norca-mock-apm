package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/bacanoo-io/norca-mock-apm/pkg/instance"
	"github.com/bacanoo-io/norca-mock-apm/pkg/server"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current status of the mock APM server",
	RunE: func(cmd *cobra.Command, args []string) error {
		absDir, err := filepath.Abs(dataDir)
		if err != nil {
			return fmt.Errorf("invalid data dir: %w", err)
		}

		inst, err := instance.Read(absDir)
		if err != nil || inst == nil {
			fmt.Println("Status: STOPPED (no running instance found)")
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		st, statusErr := server.GetStatus(ctx, inst.AdminPort)
		cancel()

		if statusErr != nil && !instance.IsProcessAlive(inst.PID) {
			_ = instance.Remove(absDir)
			fmt.Println("Status: STOPPED (cleaned up stale pid file)")
			return nil
		}

		fmt.Println("==================================================")
		fmt.Println("  NORCA Mock APM Server - Status")
		fmt.Println("==================================================")
		fmt.Printf("  Status:      RUNNING\n")
		fmt.Printf("  PID:         %d\n", inst.PID)
		fmt.Printf("  gRPC Port:   :%d\n", inst.GRPCPort)
		fmt.Printf("  Admin Port:  127.0.0.1:%d\n", inst.AdminPort)
		fmt.Printf("  Data Dir:    %s\n", inst.DataDir)
		if st != nil {
			fmt.Printf("  Uptime:      %.1fs\n", st.UptimeSeconds)
			fmt.Println("--------------------------------------------------")
			fmt.Println("  Collected Telemetry Batches:")
			fmt.Printf("    Logs:      %d\n", st.Stats.LogsCount)
			fmt.Printf("    Metrics:   %d\n", st.Stats.MetricsCount)
			fmt.Printf("    Traces:    %d\n", st.Stats.TracesCount)
			if !st.Stats.LastReceivedAt.IsZero() {
				fmt.Printf("    Last Seen: %s\n", st.Stats.LastReceivedAt.Format(time.RFC3339))
			}
		}
		fmt.Println("==================================================")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

