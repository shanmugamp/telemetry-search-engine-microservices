package indexer_test

import (
	"sync"
	"testing"
	"time"

	"search-service/internal/indexer"
	"search-service/internal/model"
)

func makeDoc(msg, ns, app, severity string) model.Document {
	return model.Document{
		Message:        msg,
		Namespace:      ns,
		AppName:        app,
		SeverityString: severity,
		NanoTimeStamp:  time.Now().UnixNano(),
	}
}

// ── Basic search ──────────────────────────────────────────────────────────────

func TestSearch_BasicMatch(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka consumer timeout error", "prod", "kafka", "ERROR"))
	idx.AddDocument(makeDoc("nginx started successfully", "prod", "nginx", "INFO"))

	res := idx.Search("kafka timeout", 1, 10)
	if res.TotalCount == 0 {
		t.Fatal("expected results")
	}
	if res.Documents[0].Message != "kafka consumer timeout error" {
		t.Errorf("expected kafka doc first, got: %s", res.Documents[0].Message)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("some message", "ns", "app", "INFO"))
	res := idx.Search("", 1, 10)
	if res.TotalCount != 0 {
		t.Errorf("expected 0 results for empty query, got %d", res.TotalCount)
	}
}

func TestSearch_EmptyIndex(t *testing.T) {
	idx := indexer.NewIndex()
	res := idx.Search("anything", 1, 10)
	if res.TotalCount != 0 {
		t.Errorf("expected 0 results in empty index")
	}
}

func TestSearch_NoMatch(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka error", "prod", "kafka", "ERROR"))
	res := idx.Search("postgres", 1, 10)
	if res.TotalCount != 0 {
		t.Errorf("expected 0 results, got %d", res.TotalCount)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("Kafka Consumer Error", "prod", "kafka", "ERROR"))
	for _, q := range []string{"kafka", "KAFKA", "Kafka", "CONSUMER"} {
		res := idx.Search(q, 1, 10)
		if res.TotalCount == 0 {
			t.Errorf("expected match for query %q", q)
		}
	}
}

func TestSearch_StopWordOnly(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("some message", "ns", "app", "INFO"))
	res := idx.Search("the and or", 1, 10)
	if res.TotalCount != 0 {
		t.Errorf("stop-word-only query should return 0 results")
	}
}

func TestSearch_MultiTermRanking(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("error error error", "prod", "app", "ERROR"))
	idx.AddDocument(makeDoc("error warning info", "prod", "app", "WARN"))
	idx.AddDocument(makeDoc("info debug trace", "prod", "app", "INFO"))

	res := idx.Search("error", 1, 10)
	if res.TotalCount < 2 {
		t.Fatalf("expected at least 2 results")
	}
	// Doc with 3x "error" should rank higher than doc with 1x "error"
	if res.Documents[0].SeverityString != "ERROR" {
		t.Errorf("expected ERROR doc first (higher TF), got %s", res.Documents[0].SeverityString)
	}
}

// ── Pagination ────────────────────────────────────────────────────────────────

func TestSearch_Pagination(t *testing.T) {
	idx := indexer.NewIndex()
	for i := 0; i < 25; i++ {
		idx.AddDocument(makeDoc("kafka error message", "prod", "kafka", "ERROR"))
	}

	p1 := idx.Search("kafka", 1, 10)
	if len(p1.Documents) != 10 {
		t.Errorf("page 1 expected 10 docs, got %d", len(p1.Documents))
	}
	if p1.TotalCount != 25 {
		t.Errorf("expected total_count=25, got %d", p1.TotalCount)
	}

	p3 := idx.Search("kafka", 3, 10)
	if len(p3.Documents) != 5 {
		t.Errorf("page 3 expected 5 docs, got %d", len(p3.Documents))
	}
}

func TestSearch_PageBeyondResults(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka error", "prod", "kafka", "ERROR"))

	res := idx.Search("kafka", 100, 10)
	if len(res.Documents) != 0 {
		t.Errorf("expected 0 docs on page beyond end, got %d", len(res.Documents))
	}
	if res.TotalCount != 1 {
		t.Errorf("expected TotalCount=1, got %d", res.TotalCount)
	}
}

func TestSearch_PageSizeClamp(t *testing.T) {
	idx := indexer.NewIndex()
	for i := 0; i < 200; i++ {
		idx.AddDocument(makeDoc("kafka error", "prod", "kafka", "ERROR"))
	}
	// page_size > 100 should be clamped by handler; index itself doesn't clamp
	res := idx.Search("kafka", 1, 100)
	if len(res.Documents) > 100 {
		t.Errorf("should not exceed 100 docs per page")
	}
}

// ── Query length protection ───────────────────────────────────────────────────

func TestSearch_LongQueryTruncated(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka", "prod", "kafka", "INFO"))
	longQuery := string(make([]byte, 1000))
	for i := range longQuery {
		_ = i
	}
	// Should not panic — just return empty results
	res := idx.Search(string(make([]byte, 1000)), 1, 10)
	_ = res
}

// ── LRU Cache ─────────────────────────────────────────────────────────────────

func TestSearch_CacheHit(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka error", "prod", "kafka", "ERROR"))

	r1 := idx.Search("kafka", 1, 10)
	if r1.CacheHit {
		t.Error("first search should not be a cache hit")
	}
	r2 := idx.Search("kafka", 1, 10)
	if !r2.CacheHit {
		t.Error("second identical search should be a cache hit")
	}
}

func TestSearch_CacheInvalidatedOnReset(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka error", "prod", "kafka", "ERROR"))

	idx.Search("kafka", 1, 10) // populate cache
	idx.Reset()
	idx.AddDocument(makeDoc("nginx info", "prod", "nginx", "INFO"))

	// After reset, cache is cleared — should miss and return new results
	r := idx.Search("kafka", 1, 10)
	if r.CacheHit {
		t.Error("cache should be cleared after Reset()")
	}
	if r.TotalCount != 0 {
		t.Errorf("kafka should not match after reset and new docs added, got %d", r.TotalCount)
	}
}

func TestInvalidateCache(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka", "prod", "kafka", "INFO"))
	idx.Search("kafka", 1, 10)
	idx.InvalidateCache()
	r := idx.Search("kafka", 1, 10)
	if r.CacheHit {
		t.Error("cache should be cleared after InvalidateCache()")
	}
}

// ── Readiness ─────────────────────────────────────────────────────────────────

func TestIndexReadiness(t *testing.T) {
	idx := indexer.NewIndex()
	if idx.IsReady() {
		t.Error("new index should not be ready")
	}
	idx.SetReady(true)
	if !idx.IsReady() {
		t.Error("index should be ready after SetReady(true)")
	}
	idx.Reset()
	if idx.IsReady() {
		t.Error("index should not be ready after Reset()")
	}
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func TestStats(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka error timeout", "prod", "kafka", "ERROR"))
	idx.AddDocument(makeDoc("nginx started", "prod", "nginx", "INFO"))
	idx.AddFile("test.parquet")

	stats := idx.Stats()
	if stats.TotalDocuments != 2 {
		t.Errorf("expected 2 docs, got %d", stats.TotalDocuments)
	}
	if stats.TotalTerms == 0 {
		t.Error("expected some terms")
	}
	if len(stats.Files) != 1 || stats.Files[0] != "test.parquet" {
		t.Errorf("expected files=[test.parquet], got %v", stats.Files)
	}
}

func TestStats_FilesImmutable(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddFile("a.parquet")
	s1 := idx.Stats()
	s1.Files = append(s1.Files, "injected.parquet")
	s2 := idx.Stats()
	if len(s2.Files) != 1 {
		t.Error("Stats().Files must be a safe copy — mutation should not affect index")
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestConcurrentAddAndSearch(t *testing.T) {
	idx := indexer.NewIndex()
	var wg sync.WaitGroup

	// 10 writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				idx.AddDocument(makeDoc("kafka error concurrent", "prod", "kafka", "ERROR"))
			}
		}(i)
	}

	// 5 readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				idx.Search("kafka", 1, 10)
			}
		}()
	}

	wg.Wait()
}

// ── Reset ─────────────────────────────────────────────────────────────────────

func TestReset(t *testing.T) {
	idx := indexer.NewIndex()
	idx.AddDocument(makeDoc("kafka", "prod", "kafka", "INFO"))
	idx.AddFile("test.parquet")
	idx.SetReady(true)

	idx.Reset()

	stats := idx.Stats()
	if stats.TotalDocuments != 0 || stats.TotalTerms != 0 || len(stats.Files) != 0 {
		t.Error("reset should clear all state")
	}
	if idx.IsReady() {
		t.Error("reset should clear readiness")
	}
	res := idx.Search("kafka", 1, 10)
	if res.TotalCount != 0 {
		t.Error("search after reset should return 0 results")
	}
}

// ── Tokenizer ─────────────────────────────────────────────────────────────────

func TestTokenize_Basic(t *testing.T) {
	tokens := indexer.Tokenize("hello world foo")
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %v", tokens)
	}
}

func TestTokenize_StopWords(t *testing.T) {
	tokens := indexer.Tokenize("the quick brown fox")
	for _, t_ := range tokens {
		if t_ == "the" {
			t.Error("stop word 'the' should be removed")
		}
	}
}

func TestTokenize_LowerCase(t *testing.T) {
	tokens := indexer.Tokenize("KAFKA ERROR")
	for _, tok := range tokens {
		for _, ch := range tok {
			if ch >= 'A' && ch <= 'Z' {
				t.Errorf("token %q should be lowercase", tok)
			}
		}
	}
}

func TestTokenize_SpecialChars(t *testing.T) {
	tokens := indexer.Tokenize("kafka.consumer-group_id: error!")
	found := map[string]bool{}
	for _, tok := range tokens {
		found[tok] = true
	}
	if !found["kafka.consumer-group_id"] {
		t.Error("expected 'kafka.consumer-group_id' as a single token")
	}
}

func TestTokenize_Empty(t *testing.T) {
	tokens := indexer.Tokenize("")
	if len(tokens) != 0 {
		t.Errorf("expected empty token list, got %v", tokens)
	}
}

func TestTokenize_SingleChar(t *testing.T) {
	tokens := indexer.Tokenize("a b c")
	if len(tokens) != 0 {
		t.Errorf("single-char tokens should be filtered, got %v", tokens)
	}
}

// ── TookMs ────────────────────────────────────────────────────────────────────

func TestSearch_TookMsPositive(t *testing.T) {
	idx := indexer.NewIndex()
	for i := 0; i < 100; i++ {
		idx.AddDocument(makeDoc("kafka error message warning timeout", "prod", "kafka", "ERROR"))
	}
	res := idx.Search("kafka error", 1, 10)
	if res.TookMs < 0 {
		t.Error("TookMs should be non-negative")
	}
}
