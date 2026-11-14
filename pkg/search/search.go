package search

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"time"
)

// QueryOptions defines the parameters for filtering and searching telemetry records.
type QueryOptions struct {
	Category    string    `json:"category"`    // "logs", "metrics", "traces", or "all" / ""
	Query       string    `json:"query"`       // Search query (text terms, regex, or semantic sentence)
	Mode        string    `json:"mode"`        // "text" (default), "vector" (TF-IDF cosine similarity), or "hybrid"
	ServiceName string    `json:"service"`     // Filter by resource service.name
	Severity    string    `json:"severity"`    // Filter by log severity (e.g. "ERROR", "WARN")
	TraceID     string    `json:"trace_id"`    // Filter by specific traceId
	Since       time.Time `json:"since"`       // Filter records received after this time
	Until       time.Time `json:"until"`       // Filter records received before this time
	MinScore    float64   `json:"min_score"`   // Minimum score for vector/similarity search (0.0 to 1.0)
	Limit       int       `json:"limit"`       // Maximum number of results to return
}

// ResultItem represents a single matched record with its relevance score.
type ResultItem struct {
	Category string          `json:"category"`
	Score    float64         `json:"score"`
	Record   json.RawMessage `json:"record"`
}

// Vectorizer handles tokenization, TF-IDF calculation, and cosine similarity.
type Vectorizer struct{}

func NewVectorizer() *Vectorizer {
	return &Vectorizer{}
}

// Tokenize splits text into normalized lower-case tokens, ignoring punctuation.
func (v *Vectorizer) Tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// TermFrequencies builds a frequency map for a list of tokens.
func (v *Vectorizer) TermFrequencies(tokens []string) map[string]float64 {
	tf := make(map[string]float64)
	if len(tokens) == 0 {
		return tf
	}
	for _, t := range tokens {
		tf[t]++
	}
	total := float64(len(tokens))
	for t := range tf {
		tf[t] = tf[t] / total
	}
	return tf
}

// CosineSimilarity calculates the cosine similarity between two term-frequency vectors.
func (v *Vectorizer) CosineSimilarity(vecA, vecB map[string]float64) float64 {
	if len(vecA) == 0 || len(vecB) == 0 {
		return 0.0
	}

	var dotProduct, normA, normB float64

	for term, valA := range vecA {
		normA += valA * valA
		if valB, ok := vecB[term]; ok {
			dotProduct += valA * valB
		}
	}

	for _, valB := range vecB {
		normB += valB * valB
	}

	if normA == 0.0 || normB == 0.0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// MatchesMetadata filters by service name, severity, traceId, and timestamps.
func MatchesMetadata(recordStr string, opt QueryOptions) bool {
	if opt.ServiceName != "" {
		if !strings.Contains(recordStr, `"service.name"`) && !strings.Contains(recordStr, opt.ServiceName) {
			return false
		}
		// Check explicit pattern
		pattern := `"(?:key|name)":\s*"service\.name"[^}]+"stringValue":\s*"` + regexp.QuoteMeta(opt.ServiceName) + `"`
		if matched, _ := regexp.MatchString(pattern, recordStr); !matched && !strings.Contains(recordStr, opt.ServiceName) {
			return false
		}
	}

	if opt.TraceID != "" {
		if !strings.Contains(recordStr, opt.TraceID) {
			return false
		}
	}

	if opt.Severity != "" {
		upSev := strings.ToUpper(opt.Severity)
		if !strings.Contains(strings.ToUpper(recordStr), upSev) {
			return false
		}
	}

	if !opt.Since.IsZero() || !opt.Until.IsZero() {
		// Look for received_at timestamp in _mock_meta
		if idx := strings.Index(recordStr, `"received_at":"`); idx != -1 {
			start := idx + len(`"received_at":"`)
			end := strings.IndexByte(recordStr[start:], '"')
			if end != -1 {
				timeStr := recordStr[start : start+end]
				if t, err := time.Parse(time.RFC3339Nano, timeStr); err == nil {
					if !opt.Since.IsZero() && t.Before(opt.Since) {
						return false
					}
					if !opt.Until.IsZero() && t.After(opt.Until) {
						return false
					}
				}
			}
		}
	}

	return true
}

// ScoreText evaluates a record against the search query using text or vector modes.
func ScoreText(recordStr string, opt QueryOptions, vec *Vectorizer) (float64, bool) {
	if opt.Query == "" {
		return 1.0, true
	}

	switch strings.ToLower(opt.Mode) {
	case "vector":
		// Vector mode: Cosine similarity of term vectors
		queryTokens := vec.Tokenize(opt.Query)
		recordTokens := vec.Tokenize(recordStr)
		if len(queryTokens) == 0 || len(recordTokens) == 0 {
			return 0.0, false
		}

		queryVec := vec.TermFrequencies(queryTokens)
		recordVec := vec.TermFrequencies(recordTokens)
		sim := vec.CosineSimilarity(queryVec, recordVec)

		minScore := opt.MinScore
		if minScore <= 0 {
			minScore = 0.05 // default subtle threshold
		}

		if sim >= minScore {
			return sim, true
		}
		return 0.0, false

	case "hybrid":
		// Hybrid: Must contain at least one term or match regex, scored by vector similarity
		lowerRec := strings.ToLower(recordStr)
		queryTokens := vec.Tokenize(opt.Query)

		hasTerm := false
		for _, qt := range queryTokens {
			if strings.Contains(lowerRec, qt) {
				hasTerm = true
				break
			}
		}
		if !hasTerm {
			return 0.0, false
		}

		queryVec := vec.TermFrequencies(queryTokens)
		recordVec := vec.TermFrequencies(vec.Tokenize(recordStr))
		sim := vec.CosineSimilarity(queryVec, recordVec)
		return sim, true

	default: // "text" mode
		// Check for regex: prefix
		if strings.HasPrefix(opt.Query, "regex:") {
			rePattern := strings.TrimPrefix(opt.Query, "regex:")
			matched, err := regexp.MatchString(rePattern, recordStr)
			if err == nil && matched {
				return 1.0, true
			}
			return 0.0, false
		}

		// Case-insensitive substring match
		if strings.Contains(strings.ToLower(recordStr), strings.ToLower(opt.Query)) {
			return 1.0, true
		}
		return 0.0, false
	}
}

