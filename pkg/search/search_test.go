package search

import (
	"testing"
	"time"
)

func TestVectorizer_TokenizeAndCosineSimilarity(t *testing.T) {
	vec := NewVectorizer()

	tokens1 := vec.Tokenize("Database connection timed out after 30 seconds")
	tokens2 := vec.Tokenize("Connection to database failed due to timeout")
	tokens3 := vec.Tokenize("User logged in successfully from mobile app")

	tf1 := vec.TermFrequencies(tokens1)
	tf2 := vec.TermFrequencies(tokens2)
	tf3 := vec.TermFrequencies(tokens3)

	sim12 := vec.CosineSimilarity(tf1, tf2)
	sim13 := vec.CosineSimilarity(tf1, tf3)

	if sim12 <= 0.2 {
		t.Fatalf("expected high similarity between related texts, got: %f", sim12)
	}

	if sim13 >= sim12 {
		t.Fatalf("expected unrelated text to have lower similarity (%f) than related (%f)", sim13, sim12)
	}
}

func TestScoreText_Modes(t *testing.T) {
	vec := NewVectorizer()
	record := `{"body":{"stringValue":"Failed to connect to postgresql database at 10.0.0.1:5432"}}`

	// 1. Text mode substring
	score, ok := ScoreText(record, QueryOptions{Query: "postgresql", Mode: "text"}, vec)
	if !ok || score != 1.0 {
		t.Fatalf("expected text substring match, got ok=%v, score=%f", ok, score)
	}

	// 2. Text mode regex
	score, ok = ScoreText(record, QueryOptions{Query: `regex:10\.0\.0\.\d+:\d+`, Mode: "text"}, vec)
	if !ok || score != 1.0 {
		t.Fatalf("expected regex match, got ok=%v, score=%f", ok, score)
	}

	// 3. Vector mode
	score, ok = ScoreText(record, QueryOptions{Query: "database postgres connection error", Mode: "vector", MinScore: 0.1}, vec)
	if !ok || score < 0.1 {
		t.Fatalf("expected vector match with score >= 0.1, got ok=%v, score=%f", ok, score)
	}

	// 4. Hybrid mode
	score, ok = ScoreText(record, QueryOptions{Query: "postgresql database", Mode: "hybrid"}, vec)
	if !ok || score <= 0.0 {
		t.Fatalf("expected hybrid match, got ok=%v, score=%f", ok, score)
	}

	// 5. Non-matching query
	_, ok = ScoreText(record, QueryOptions{Query: "redis cache cluster", Mode: "hybrid"}, vec)
	if ok {
		t.Fatalf("expected non-match for unrelated query in hybrid mode")
	}
}

func TestMatchesMetadata(t *testing.T) {
	record := `{"_mock_meta":{"received_at":"2026-11-14T20:00:00.000Z"},"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"order-service"}}]},"severityText":"ERROR","traceId":"4bf92f3577b34da6a3ce929d0e0e4736"}`

	// Match service
	if !MatchesMetadata(record, QueryOptions{ServiceName: "order-service"}) {
		t.Fatalf("expected service match")
	}
	if MatchesMetadata(record, QueryOptions{ServiceName: "payment-service"}) {
		t.Fatalf("expected service mismatch")
	}

	// Match severity
	if !MatchesMetadata(record, QueryOptions{Severity: "error"}) {
		t.Fatalf("expected severity match")
	}
	if MatchesMetadata(record, QueryOptions{Severity: "debug"}) {
		t.Fatalf("expected severity mismatch")
	}

	// Match traceId
	if !MatchesMetadata(record, QueryOptions{TraceID: "4bf92f3577b34da6"}) {
		t.Fatalf("expected traceId match")
	}

	// Match timestamps
	since := time.Date(2026, 11, 14, 19, 0, 0, 0, time.UTC)
	until := time.Date(2026, 11, 14, 21, 0, 0, 0, time.UTC)
	if !MatchesMetadata(record, QueryOptions{Since: since, Until: until}) {
		t.Fatalf("expected time range match")
	}

	pastUntil := time.Date(2026, 11, 14, 19, 30, 0, 0, time.UTC)
	if MatchesMetadata(record, QueryOptions{Until: pastUntil}) {
		t.Fatalf("expected time out of range")
	}
}
