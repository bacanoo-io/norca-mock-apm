package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bacanoo-io/norca-mock-apm/pkg/instance"
	"github.com/bacanoo-io/norca-mock-apm/pkg/search"
	"github.com/bacanoo-io/norca-mock-apm/pkg/server"
	"github.com/bacanoo-io/norca-mock-apm/pkg/storage"
	"github.com/spf13/cobra"
)

var (
	searchCategory string
	searchMode     string
	searchService  string
	searchSeverity string
	searchTraceID  string
	searchMinScore float64
	searchLimit    int
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Complex text and vector search across collected telemetry",
	Long: `Search through collected logs, metrics, and traces using text matching,
regex, or vector-based TF-IDF cosine similarity.

Modes:
  text   - Substring match or regex (e.g. 'regex:.*5\\d\\d.*')
  vector - Semantic vector similarity (TF-IDF & Cosine Similarity)
  hybrid - Keyword presence combined with semantic vector scoring

Examples:
  norca-mock-apm search "database connection failed" --mode vector
  norca-mock-apm search "regex:status_code\":\\s*500" --category traces
  norca-mock-apm search "auth timeout" --service auth-service --limit 5
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) > 0 {
			query = strings.Join(args, " ")
		}

		opt := search.QueryOptions{
			Category:    searchCategory,
			Query:       query,
			Mode:        searchMode,
			ServiceName: searchService,
			Severity:    searchSeverity,
			TraceID:     searchTraceID,
			MinScore:    searchMinScore,
			Limit:       searchLimit,
		}

		absDir, err := filepath.Abs(dataDir)
		if err != nil {
			return fmt.Errorf("invalid data dir: %w", err)
		}

		var results []search.ResultItem

		// Attempt to search via running instance Admin API
		inst, err := instance.Read(absDir)
		if err == nil && inst != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			sResp, sErr := server.SearchRecords(ctx, inst.AdminPort, opt)
			cancel()

			if sErr == nil && sResp != nil {
				results = sResp.Results
			}
		}

		// Fallback: search directly on disk
		if results == nil {
			stor, err := storage.NewStorage(absDir, includeMeta)
			if err != nil {
				return fmt.Errorf("failed to open storage for search: %w", err)
			}
			res, err := stor.Search(opt)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}
			results = res
		}

		fmt.Println("==================================================")
		fmt.Printf("  NORCA Mock APM Search (Found %d matches)\n", len(results))
		fmt.Printf("  Query: '%s' | Mode: %s | Category: %s\n", query, searchMode, searchCategory)
		fmt.Println("==================================================")

		if len(results) == 0 {
			fmt.Println("No matching telemetry records found.")
			return nil
		}

		for i, r := range results {
			fmt.Printf("\n[%d] Category: %s | Score: %.3f\n", i+1, strings.ToUpper(r.Category), r.Score)

			// Pretty print JSON preview
			var prettyJSON map[string]any
			if err := json.Unmarshal(r.Record, &prettyJSON); err == nil {
				if meta, ok := prettyJSON["_mock_meta"].(map[string]any); ok {
					if ts, ok := meta["received_at"]; ok {
						fmt.Printf("    Received: %v\n", ts)
					}
				}
				previewBytes, _ := json.MarshalIndent(prettyJSON, "    ", "  ")
				// Truncate preview if very long
				previewStr := string(previewBytes)
				lines := strings.Split(previewStr, "\n")
				if len(lines) > 20 {
					previewStr = strings.Join(lines[:20], "\n") + "\n    ... (truncated)"
				}
				fmt.Println("    " + previewStr)
			} else {
				fmt.Printf("    %s\n", string(r.Record))
			}
		}

		fmt.Println("\n==================================================")
		return nil
	},
}

func init() {
	searchCmd.Flags().StringVarP(&searchCategory, "category", "c", "all", "Category to search: 'logs', 'metrics', 'traces', or 'all'")
	searchCmd.Flags().StringVarP(&searchMode, "mode", "m", "text", "Search mode: 'text', 'vector', or 'hybrid'")
	searchCmd.Flags().StringVarP(&searchService, "service", "s", "", "Filter by service.name")
	searchCmd.Flags().StringVar(&searchSeverity, "severity", "", "Filter by log severity (e.g. ERROR)")
	searchCmd.Flags().StringVar(&searchTraceID, "trace-id", "", "Filter by traceId")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0.05, "Minimum score threshold for vector similarity (0.0 to 1.0)")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "l", 10, "Maximum number of results to display")

	rootCmd.AddCommand(searchCmd)
}

