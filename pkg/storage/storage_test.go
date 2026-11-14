package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestStorage_SaveAndReset(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "norca-storage-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	stor, err := NewStorage(tempDir, true)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	ctx := context.Background()
	dummyMeta := map[string][]string{
		"authorization": {"Bearer test-token"},
		"x-test-header": {"custom-value"},
	}

	// 1. Save Logs
	logReq := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "test-service"}}},
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{
						LogRecords: []*logspb.LogRecord{
							{Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "Hello mock log!"}}},
						},
					},
				},
			},
		},
	}
	if err := stor.SaveLogs(ctx, logReq, dummyMeta, "127.0.0.1:12345"); err != nil {
		t.Fatalf("SaveLogs failed: %v", err)
	}

	// 2. Save Metrics
	metricReq := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "test-service"}}},
					},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Metrics: []*metricspb.Metric{
							{Name: "test_counter", Description: "Dummy metric"},
						},
					},
				},
			},
		},
	}
	if err := stor.SaveMetrics(ctx, metricReq, dummyMeta, "127.0.0.1:12345"); err != nil {
		t.Fatalf("SaveMetrics failed: %v", err)
	}

	// 3. Save Traces
	traceReq := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "test-service"}}},
					},
				},
				ScopeSpans: []*tracepb.ScopeSpans{
					{
						Spans: []*tracepb.Span{
							{Name: "HTTP GET /health"},
						},
					},
				},
			},
		},
	}
	if err := stor.SaveTraces(ctx, traceReq, dummyMeta, "127.0.0.1:12345"); err != nil {
		t.Fatalf("SaveTraces failed: %v", err)
	}

	// Verify stats
	stats := stor.GetStats()
	if stats.LogsCount != 1 || stats.MetricsCount != 1 || stats.TracesCount != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	// Test Query API
	logs, err := stor.ReadRecords("logs", 10)
	if err != nil {
		t.Fatalf("ReadRecords logs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(logs))
	}
	if !strings.Contains(string(logs[0]), "Hello mock log!") {
		t.Fatalf("log record does not contain expected body: %s", string(logs[0]))
	}

	// Verify files exist and have content
	for _, sub := range []string{"logs", "metrics", "traces"} {
		file := filepath.Join(tempDir, sub, "current.jsonl")
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", file, err)
		}
		if len(content) == 0 {
			t.Fatalf("file %s is empty", file)
		}
		if !strings.Contains(string(content), "_mock_meta") {
			t.Fatalf("file %s does not contain _mock_meta", file)
		}
	}

	// 4. Test Reset
	if err := stor.Reset(); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	resetStats := stor.GetStats()
	if resetStats.LogsCount != 0 || resetStats.MetricsCount != 0 || resetStats.TracesCount != 0 {
		t.Fatalf("stats not zeroed after reset: %+v", resetStats)
	}

	// Check that current.jsonl files were cleared
	for _, sub := range []string{"logs", "metrics", "traces"} {
		file := filepath.Join(tempDir, sub, "current.jsonl")
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("file %s still exists after reset", file)
		}
	}
}

func TestStorage_ConcurrentWrites(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "norca-concurrent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	stor, err := NewStorage(tempDir, true)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	workers := 10
	iterations := 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				logReq := &collogspb.ExportLogsServiceRequest{
					ResourceLogs: []*logspb.ResourceLogs{
						{
							ScopeLogs: []*logspb.ScopeLogs{
								{
									LogRecords: []*logspb.LogRecord{
										{Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: fmt.Sprintf("worker-%d-msg-%d", workerID, j)}}},
									},
								},
							},
						},
					},
				}
				_ = stor.SaveLogs(ctx, logReq, nil, "127.0.0.1:0")
			}
		}(i)
	}

	wg.Wait()

	stats := stor.GetStats()
	expected := int64(workers * iterations)
	if stats.LogsCount != expected {
		t.Fatalf("expected %d logs count, got %d", expected, stats.LogsCount)
	}

	records, err := stor.ReadRecords("logs", 0)
	if err != nil {
		t.Fatalf("ReadRecords failed: %v", err)
	}
	if len(records) != int(expected) {
		t.Fatalf("expected %d records from file, got %d", expected, len(records))
	}
}
