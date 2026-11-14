package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bacanoo-io/norca-mock-apm/pkg/search"
	"github.com/bacanoo-io/norca-mock-apm/pkg/storage"
)

type StatusResponse struct {
	Status        string        `json:"status"`
	PID           int           `json:"pid"`
	GRPCPort      int           `json:"grpc_port"`
	AdminPort     int           `json:"admin_port"`
	UptimeSeconds float64       `json:"uptime_seconds"`
	StartedAt     time.Time     `json:"started_at"`
	Stats         storage.Stats `json:"stats"`
}

type QueryResponse struct {
	Category string            `json:"category"`
	Count    int               `json:"count"`
	Records  []json.RawMessage `json:"records"`
}

type SearchResponse struct {
	Count   int                 `json:"count"`
	Results []search.ResultItem `json:"results"`
}

type AdminServer struct {
	httpServer *http.Server
	storage    *storage.Storage
	grpcPort   int
	adminPort  int
	pid        int
	startedAt  time.Time
	shutdownCh chan struct{}
	mu         sync.Mutex
}

func NewAdminServer(adminPort, grpcPort, pid int, s *storage.Storage, shutdownCh chan struct{}) *AdminServer {
	srv := &AdminServer{
		storage:    s,
		grpcPort:   grpcPort,
		adminPort:  adminPort,
		pid:        pid,
		startedAt:  time.Now().UTC(),
		shutdownCh: shutdownCh,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", srv.handleStatus)
	mux.HandleFunc("/reset", srv.handleReset)
	mux.HandleFunc("/shutdown", srv.handleShutdown)

	// Test Assertion & Query API
	mux.HandleFunc("/api/v1/logs", srv.handleQuery("logs"))
	mux.HandleFunc("/api/v1/metrics", srv.handleQuery("metrics"))
	mux.HandleFunc("/api/v1/traces", srv.handleQuery("traces"))

	// Complex Text & Vector Search API
	mux.HandleFunc("/api/v1/search", srv.handleSearch)

	srv.httpServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", adminPort),
		Handler: mux,
	}

	return srv
}

func (s *AdminServer) Serve(listener net.Listener) error {
	return s.httpServer.Serve(listener)
}

func (s *AdminServer) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *AdminServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	res := StatusResponse{
		Status:        "running",
		PID:           s.pid,
		GRPCPort:      s.grpcPort,
		AdminPort:     s.adminPort,
		UptimeSeconds: time.Since(s.startedAt).Seconds(),
		StartedAt:     s.startedAt,
		Stats:         s.storage.GetStats(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *AdminServer) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := s.storage.Reset(); err != nil {
		http.Error(w, fmt.Sprintf("failed to reset storage: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "storage successfully cleared",
	})
}

func (s *AdminServer) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "server shutting down",
	})

	go func() {
		time.Sleep(100 * time.Millisecond)
		select {
		case s.shutdownCh <- struct{}{}:
		default:
		}
	}()
}

func (s *AdminServer) handleQuery(category string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		limit := 0
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		records, err := s.storage.ReadRecords(category, limit)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read records: %v", err), http.StatusInternalServerError)
			return
		}

		if records == nil {
			records = []json.RawMessage{}
		}

		resp := QueryResponse{
			Category: category,
			Count:    len(records),
			Records:  records,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *AdminServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	var opt search.QueryOptions

	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		opt.Query = q.Get("q")
		opt.Category = q.Get("category")
		opt.Mode = q.Get("mode")
		opt.ServiceName = q.Get("service")
		opt.Severity = q.Get("severity")
		opt.TraceID = q.Get("trace_id")

		if lStr := q.Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil {
				opt.Limit = l
			}
		}
		if msStr := q.Get("min_score"); msStr != "" {
			if ms, err := strconv.ParseFloat(msStr, 64); err == nil {
				opt.MinScore = ms
			}
		}
	case http.MethodPost:
		if err := json.NewDecoder(r.Body).Decode(&opt); err != nil {
			http.Error(w, fmt.Sprintf("invalid json payload: %v", err), http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "method not allowed (use GET or POST)", http.StatusMethodNotAllowed)
		return
	}

	results, err := s.storage.Search(opt)
	if err != nil {
		http.Error(w, fmt.Sprintf("search failed: %v", err), http.StatusInternalServerError)
		return
	}

	if results == nil {
		results = []search.ResultItem{}
	}

	resp := SearchResponse{
		Count:   len(results),
		Results: results,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// Client functions for Admin API

func GetStatus(ctx context.Context, adminPort int) (*StatusResponse, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/status", adminPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &status, nil
}

func CallReset(ctx context.Context, adminPort int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/reset", adminPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func CallShutdown(ctx context.Context, adminPort int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/shutdown", adminPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func QueryRecords(ctx context.Context, adminPort int, category string, limit int) (*QueryResponse, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/%s?limit=%d", adminPort, category, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var qResp QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &qResp, nil
}

func SearchRecords(ctx context.Context, adminPort int, opt search.QueryOptions) (*SearchResponse, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/v1/search", adminPort)
	bodyBytes, err := json.Marshal(opt)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var sResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &sResp, nil
}
