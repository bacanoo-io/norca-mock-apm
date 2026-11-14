package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/bacanoo-io/norca-mock-apm/pkg/search"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Stats struct {
	LogsCount      int64     `json:"logs_count"`
	MetricsCount   int64     `json:"metrics_count"`
	TracesCount    int64     `json:"traces_count"`
	LastReceivedAt time.Time `json:"last_received_at,omitempty"`
}

type Storage struct {
	dataDir     string
	logsDir     string
	metricsDir  string
	tracesDir   string
	includeMeta bool

	logsMu    sync.Mutex
	metricsMu sync.Mutex
	tracesMu  sync.Mutex
	statsMu   sync.RWMutex

	stats      Stats
	vectorizer *search.Vectorizer
}

func NewStorage(dataDir string, includeMeta bool) (*Storage, error) {
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for data dir: %w", err)
	}

	s := &Storage{
		dataDir:     absDataDir,
		logsDir:     filepath.Join(absDataDir, "logs"),
		metricsDir:  filepath.Join(absDataDir, "metrics"),
		tracesDir:   filepath.Join(absDataDir, "traces"),
		includeMeta: includeMeta,
		vectorizer:  search.NewVectorizer(),
	}

	if err := s.ensureDirs(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Storage) ensureDirs() error {
	for _, dir := range []string{s.logsDir, s.metricsDir, s.tracesDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

func (s *Storage) DataDir() string {
	return s.dataDir
}

func (s *Storage) serializeToJSONL(msg proto.Message, meta map[string][]string, peerAddr string) ([]byte, error) {
	mOpts := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}

	protoBytes, err := mOpts.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("protojson marshal failed: %w", err)
	}

	if !s.includeMeta {
		return protoBytes, nil
	}

	var protoObj map[string]any
	if err := json.Unmarshal(protoBytes, &protoObj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proto json to map: %w", err)
	}

	cleanHeaders := make(map[string]any, len(meta))
	for k, v := range meta {
		if len(v) == 1 {
			cleanHeaders[k] = v[0]
		} else {
			cleanHeaders[k] = v
		}
	}

	protoObj["_mock_meta"] = map[string]any{
		"received_at": time.Now().UTC().Format(time.RFC3339Nano),
		"peer_addr":   peerAddr,
		"headers":     cleanHeaders,
	}

	lineBytes, err := json.Marshal(protoObj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal composite json: %w", err)
	}

	return lineBytes, nil
}

func (s *Storage) SaveLogs(ctx context.Context, req *collogspb.ExportLogsServiceRequest, meta map[string][]string, peerAddr string) error {
	lineBytes, err := s.serializeToJSONL(req, meta, peerAddr)
	if err != nil {
		return err
	}

	s.logsMu.Lock()
	defer s.logsMu.Unlock()

	if err := s.writeLine(s.logsDir, "current.jsonl", lineBytes); err != nil {
		return err
	}

	s.statsMu.Lock()
	s.stats.LogsCount++
	s.stats.LastReceivedAt = time.Now().UTC()
	s.statsMu.Unlock()

	return nil
}

func (s *Storage) SaveMetrics(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest, meta map[string][]string, peerAddr string) error {
	lineBytes, err := s.serializeToJSONL(req, meta, peerAddr)
	if err != nil {
		return err
	}

	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	if err := s.writeLine(s.metricsDir, "current.jsonl", lineBytes); err != nil {
		return err
	}

	s.statsMu.Lock()
	s.stats.MetricsCount++
	s.stats.LastReceivedAt = time.Now().UTC()
	s.statsMu.Unlock()

	return nil
}

func (s *Storage) SaveTraces(ctx context.Context, req *coltracepb.ExportTraceServiceRequest, meta map[string][]string, peerAddr string) error {
	lineBytes, err := s.serializeToJSONL(req, meta, peerAddr)
	if err != nil {
		return err
	}

	s.tracesMu.Lock()
	defer s.tracesMu.Unlock()

	if err := s.writeLine(s.tracesDir, "current.jsonl", lineBytes); err != nil {
		return err
	}

	s.statsMu.Lock()
	s.stats.TracesCount++
	s.stats.LastReceivedAt = time.Now().UTC()
	s.statsMu.Unlock()

	return nil
}

func (s *Storage) writeLine(dir, filename string, lineBytes []byte) error {
	filePath := filepath.Join(dir, filename)
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer f.Close()

	if _, err := f.Write(append(lineBytes, '\n')); err != nil {
		return fmt.Errorf("failed to write to file %s: %w", filePath, err)
	}

	return nil
}

func (s *Storage) Reset() error {
	s.logsMu.Lock()
	defer s.logsMu.Unlock()
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.tracesMu.Lock()
	defer s.tracesMu.Unlock()
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	for _, dir := range []string{s.logsDir, s.metricsDir, s.tracesDir} {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to clear dir %s: %w", dir, err)
		}
	}

	s.stats = Stats{}
	return s.ensureDirs()
}

func (s *Storage) GetStats() Stats {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()
	return s.stats
}

func (s *Storage) ReadRecords(category string, limit int) ([]json.RawMessage, error) {
	var targetDir string
	var mu *sync.Mutex

	switch category {
	case "logs":
		targetDir = s.logsDir
		mu = &s.logsMu
	case "metrics":
		targetDir = s.metricsDir
		mu = &s.metricsMu
	case "traces":
		targetDir = s.tracesDir
		mu = &s.tracesMu
	default:
		return nil, fmt.Errorf("invalid category: %s (must be logs, metrics, or traces)", category)
	}

	mu.Lock()
	defer mu.Unlock()

	filePath := filepath.Join(targetDir, "current.jsonl")
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []json.RawMessage{}, nil
		}
		return nil, fmt.Errorf("failed to open %s: %w", filePath, err)
	}
	defer file.Close()

	var allLines []json.RawMessage
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw := make(json.RawMessage, len(line))
		copy(raw, line)
		allLines = append(allLines, raw)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s: %w", filePath, err)
	}

	if limit <= 0 || limit >= len(allLines) {
		return allLines, nil
	}

	return allLines[len(allLines)-limit:], nil
}

// Search executes complex text, metadata, or vector search across telemetry records.
func (s *Storage) Search(opt search.QueryOptions) ([]search.ResultItem, error) {
	categories := []string{"logs", "metrics", "traces"}
	if opt.Category != "" && opt.Category != "all" {
		categories = []string{opt.Category}
	}

	var results []search.ResultItem

	for _, cat := range categories {
		records, err := s.ReadRecords(cat, 0)
		if err != nil {
			return nil, err
		}

		for _, rec := range records {
			recStr := string(rec)

			if !search.MatchesMetadata(recStr, opt) {
				continue
			}

			score, matches := search.ScoreText(recStr, opt, s.vectorizer)
			if !matches {
				continue
			}

			results = append(results, search.ResultItem{
				Category: cat,
				Score:    score,
				Record:   rec,
			})
		}
	}

	// Sort results by score in descending order
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if opt.Limit > 0 && len(results) > opt.Limit {
		results = results[:opt.Limit]
	}

	return results, nil
}
