package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	dataDir     string
	grpcPort    int
	adminPort   int
	includeMeta bool
)

var rootCmd = &cobra.Command{
	Use:   "norca-mock-apm",
	Short: "Lightweight, single-instance mock APM server for local OTel testing",
	Long: `NORCA Mock APM is a lightweight, single-instance mock server that receives
OpenTelemetry logs, metrics, and traces over gRPC (OTLP v1) and stores them
into categorized files on disk for easy assertion in tests.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&dataDir, "data-dir", "d", "./.norca-data", "Directory where telemetry data and instance files are stored")
	rootCmd.PersistentFlags().IntVarP(&grpcPort, "port", "p", 4317, "Port for OTLP gRPC collector")
	rootCmd.PersistentFlags().IntVar(&adminPort, "admin-port", 4319, "Port for local admin/control API")
	rootCmd.PersistentFlags().BoolVar(&includeMeta, "with-meta", true, "Include request metadata (headers, peer addr, timestamp) in stored JSON records")
}

