package server

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/bacanoo-io/norca-mock-apm/pkg/search"
	"github.com/bacanoo-io/norca-mock-apm/pkg/storage"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestGRPCAndAdminServerE2E(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "norca-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	stor, err := storage.NewStorage(tempDir, true)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Dynamic listeners
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen grpc: %v", err)
	}
	defer grpcListener.Close()
	grpcPort := grpcListener.Addr().(*net.TCPAddr).Port

	adminListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen admin: %v", err)
	}
	defer adminListener.Close()
	adminPort := adminListener.Addr().(*net.TCPAddr).Port

	shutdownCh := make(chan struct{}, 1)
	grpcSrv := NewGRPCServer(grpcPort, stor)
	adminSrv := NewAdminServer(adminPort, grpcPort, os.Getpid(), stor, shutdownCh)

	go func() { _ = grpcSrv.Serve(grpcListener) }()
	go func() { _ = adminSrv.Serve(adminListener) }()
	defer grpcSrv.Stop()

	time.Sleep(100 * time.Millisecond)

	// Dial gRPC server
	conn, err := grpc.NewClient(
		grpcListener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial grpc server: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Attach metadata headers
	mdCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Basic dXNlcjpwYXNz", "x-custom", "custom-header")

	// 1. Send Log with body
	logsClient := collogspb.NewLogsServiceClient(conn)
	logResp, err := logsClient.Export(mdCtx, &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "payment-gateway"}}},
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{
						LogRecords: []*logspb.LogRecord{
							{
								SeverityText: "ERROR",
								Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "Database connection failed unexpectedly"}},
							},
						},
					},
				},
			},
		},
	})
	if err != nil || logResp == nil {
		t.Fatalf("Export logs failed: %v", err)
	}

	// 2. Send Metric
	metricsClient := colmetricspb.NewMetricsServiceClient(conn)
	metricResp, err := metricsClient.Export(mdCtx, &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{}},
	})
	if err != nil || metricResp == nil {
		t.Fatalf("Export metrics failed: %v", err)
	}

	// 3. Send Trace
	traceClient := coltracepb.NewTraceServiceClient(conn)
	traceResp, err := traceClient.Export(mdCtx, &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{}},
	})
	if err != nil || traceResp == nil {
		t.Fatalf("Export traces failed: %v", err)
	}

	// 4. Test Admin GetStatus
	status, err := GetStatus(ctx, adminPort)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Stats.LogsCount != 1 || status.Stats.MetricsCount != 1 || status.Stats.TracesCount != 1 {
		t.Fatalf("unexpected stats from admin status: %+v", status.Stats)
	}

	// 5. Test Assertion Query API (/api/v1/traces)
	tracesQuery, err := QueryRecords(ctx, adminPort, "traces", 10)
	if err != nil {
		t.Fatalf("QueryRecords traces failed: %v", err)
	}
	if tracesQuery.Count != 1 || len(tracesQuery.Records) != 1 {
		t.Fatalf("expected 1 trace record, got count=%d, records=%d", tracesQuery.Count, len(tracesQuery.Records))
	}

	// 6. Test Complex Text Search API (/api/v1/search)
	textSearch, err := SearchRecords(ctx, adminPort, search.QueryOptions{
		Query:       "Database connection",
		Mode:        "text",
		ServiceName: "payment-gateway",
	})
	if err != nil {
		t.Fatalf("SearchRecords text failed: %v", err)
	}
	if textSearch.Count != 1 {
		t.Fatalf("expected 1 text search match, got %d", textSearch.Count)
	}

	// 7. Test Vector / Semantic Search API
	vectorSearch, err := SearchRecords(ctx, adminPort, search.QueryOptions{
		Query:    "database sql connection error timeout",
		Mode:     "vector",
		MinScore: 0.1,
	})
	if err != nil {
		t.Fatalf("SearchRecords vector failed: %v", err)
	}
	if vectorSearch.Count != 1 {
		t.Fatalf("expected 1 vector search match, got %d", vectorSearch.Count)
	}

	// 8. Test Admin Reset
	if err := CallReset(ctx, adminPort); err != nil {
		t.Fatalf("CallReset failed: %v", err)
	}

	statusAfterReset, err := GetStatus(ctx, adminPort)
	if err != nil {
		t.Fatalf("GetStatus after reset failed: %v", err)
	}
	if statusAfterReset.Stats.LogsCount != 0 || statusAfterReset.Stats.MetricsCount != 0 || statusAfterReset.Stats.TracesCount != 0 {
		t.Fatalf("stats not zero after reset: %+v", statusAfterReset.Stats)
	}

	// 9. Test Admin Shutdown
	if err := CallShutdown(ctx, adminPort); err != nil {
		t.Fatalf("CallShutdown failed: %v", err)
	}

	select {
	case <-shutdownCh:
		// shutdown signal received successfully
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown signal not received on shutdownCh")
	}
}
