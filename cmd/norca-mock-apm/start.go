package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bacanoo-io/norca-mock-apm/pkg/instance"
	"github.com/bacanoo-io/norca-mock-apm/pkg/server"
	"github.com/bacanoo-io/norca-mock-apm/pkg/storage"
	"github.com/spf13/cobra"
)

var daemonMode bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the mock APM server",
	RunE: func(cmd *cobra.Command, args []string) error {
		absDir, err := filepath.Abs(dataDir)
		if err != nil {
			return fmt.Errorf("invalid data dir: %w", err)
		}

		// Check if already running
		if existing, err := instance.Read(absDir); err == nil && existing != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			_, statusErr := server.GetStatus(ctx, existing.AdminPort)
			cancel()

			if statusErr == nil || instance.IsProcessAlive(existing.PID) {
				return fmt.Errorf("NORCA Mock APM is already running (PID: %d, gRPC port: %d, admin port: %d)",
					existing.PID, existing.GRPCPort, existing.AdminPort)
			}
			// Stale PID file, clean it up
			_ = instance.Remove(absDir)
		}

		if daemonMode {
			return runDaemon(absDir)
		}

		return runForeground(absDir)
	},
}

func init() {
	startCmd.Flags().BoolVarP(&daemonMode, "daemon", "b", false, "Run server in the background (daemon mode)")
	rootCmd.AddCommand(startCmd)
}

func runDaemon(absDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	newArgs := []string{
		"start",
		"-d", absDir,
		"-p", fmt.Sprintf("%d", grpcPort),
		"--admin-port", fmt.Sprintf("%d", adminPort),
	}
	if !includeMeta {
		newArgs = append(newArgs, "--with-meta=false")
	}

	cmd := exec.Command(exe, newArgs...)
	cmd.Dir = filepath.Dir(absDir)
	instance.SetDaemonProcAttr(cmd)

	logFile, err := os.OpenFile(filepath.Join(absDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start background process: %w", err)
	}

	fmt.Printf("Starting NORCA Mock APM in background (PID: %d)...\n", cmd.Process.Pid)

	// Wait for admin port to respond
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		st, err := server.GetStatus(ctx, adminPort)
		cancel()
		if err == nil && st != nil {
			fmt.Printf("NORCA Mock APM started successfully.\n")
			fmt.Printf("  PID:        %d\n", st.PID)
			fmt.Printf("  gRPC Port:  %d\n", st.GRPCPort)
			fmt.Printf("  Admin Port: %d\n", st.AdminPort)
			fmt.Printf("  Data Dir:   %s\n", absDir)
			return nil
		}
	}

	return fmt.Errorf("server process started (PID: %d), but did not respond within 5 seconds", cmd.Process.Pid)
}

func runForeground(absDir string) error {
	stor, err := storage.NewStorage(absDir, includeMeta)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		return fmt.Errorf("failed to listen on gRPC port %d: %w", grpcPort, err)
	}
	defer grpcListener.Close()

	adminListener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", adminPort))
	if err != nil {
		return fmt.Errorf("failed to listen on admin port %d: %w", adminPort, err)
	}
	defer adminListener.Close()

	pid := os.Getpid()
	instInfo := instance.Info{
		PID:       pid,
		GRPCPort:  grpcPort,
		AdminPort: adminPort,
		DataDir:   absDir,
		StartedAt: time.Now().UTC(),
	}

	if err := instance.Write(absDir, instInfo); err != nil {
		return fmt.Errorf("failed to create pid file: %w", err)
	}
	defer instance.Remove(absDir)

	shutdownCh := make(chan struct{}, 1)
	grpcSrv := server.NewGRPCServer(grpcPort, stor)
	adminSrv := server.NewAdminServer(adminPort, grpcPort, pid, stor, shutdownCh)

	go func() {
		if err := grpcSrv.Serve(grpcListener); err != nil && err != net.ErrClosed {
			fmt.Fprintf(os.Stderr, "gRPC server error: %v\n", err)
		}
	}()

	go func() {
		if err := adminSrv.Serve(adminListener); err != nil && err != net.ErrClosed {
			fmt.Fprintf(os.Stderr, "Admin server error: %v\n", err)
		}
	}()

	fmt.Println("==================================================")
	fmt.Println("  NORCA Mock APM Server - Running")
	fmt.Println("==================================================")
	fmt.Printf("  PID:        %d\n", pid)
	fmt.Printf("  gRPC Port:  :%d (OTLP v1 Ingest)\n", grpcPort)
	fmt.Printf("  Admin Port: 127.0.0.1:%d\n", adminPort)
	fmt.Printf("  Data Dir:   %s\n", absDir)
	fmt.Printf("  Metadata:   %v\n", includeMeta)
	fmt.Println("==================================================")
	fmt.Println("Press Ctrl+C or execute 'norca-mock-apm shutdown' to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		fmt.Printf("\nReceived signal (%v), shutting down...\n", sig)
	case <-shutdownCh:
		fmt.Println("\nReceived admin shutdown request, shutting down...")
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = adminSrv.Shutdown(ctx)
	grpcSrv.GracefulStop()

	fmt.Println("NORCA Mock APM stopped cleanly.")
	return nil
}
