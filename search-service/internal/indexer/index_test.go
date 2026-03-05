package indexer_test

import (
	"testing"
	"time"

	"search-service/internal/indexer"
	"search-service/internal/model"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func sq(query string) indexer.SearchQuery {
	return indexer.SearchQuery{Query: query, Page: 1, PageSize: 20}
}

func search(idx *indexer.Index, q indexer.SearchQuery) indexer.SearchResult {
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 20
	}
	return idx.Search(q)
}

func addDoc(idx *indexer.Index, doc model.Document) {
	idx.AddDocument(doc)
}

// ── basic BM25 search ─────────────────────────────────────────────────────────

func TestSearch_EmptyIndex(t *testing.T) {
	idx := indexer.NewIndex()
	idx.SetReady(true)
	r := search(idx, sq("anything"))
	if r.TotalCount != 0 {
		t.Errorf("empty index: expected 0 got %d", r.TotalCount)
	}
}

func TestSearch_SingleMatch(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "kafka consumer timeout"})
	addDoc(idx, model.Document{Message: "nginx started"})
	idx.SetReady(true)

	r := search(idx, sq("kafka"))
	if r.TotalCount != 1 {
		t.Errorf("expected 1 result for kafka, got %d", r.TotalCount)
	}
}

func TestSearch_MultipleMatches(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "kafka error one"})
	addDoc(idx, model.Document{Message: "kafka error two"})
	addDoc(idx, model.Document{Message: "nginx info"})
	idx.SetReady(true)

	r := search(idx, sq("kafka"))
	if r.TotalCount != 2 {
		t.Errorf("expected 2 kafka results, got %d", r.TotalCount)
	}
}

func TestSearch_NoMatch(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "nginx started"})
	idx.SetReady(true)

	r := search(idx, sq("kafka"))
	if r.TotalCount != 0 {
		t.Errorf("expected 0 results, got %d", r.TotalCount)
	}
}

func TestSearch_EmptyQuery_NoFilters(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "some message"})
	idx.SetReady(true)

	r := search(idx, sq(""))
	if r.TotalCount != 0 {
		t.Error("empty query with no filters should return 0")
	}
}

// ── per-field filter: Sender ──────────────────────────────────────────────────

func TestSearch_FieldFilter_Sender(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg1", Sender: "10.0.0.1"})
	addDoc(idx, model.Document{Message: "msg2", Sender: "10.0.0.2"})
	addDoc(idx, model.Document{Message: "msg3", Sender: "192.168.1.1"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Sender: "10.0.0", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("expected 2 docs with sender 10.0.0.*, got %d", r.TotalCount)
	}
}

func TestSearch_FieldFilter_Sender_ExactMatch(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg", Sender: "host-a.example.com"})
	addDoc(idx, model.Document{Message: "msg", Sender: "host-b.example.com"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Sender: "host-a.example.com", Page: 1, PageSize: 20})
	if r.TotalCount != 1 {
		t.Errorf("expected 1 exact sender match, got %d", r.TotalCount)
	}
}

// ── per-field filter: Hostname ────────────────────────────────────────────────

func TestSearch_FieldFilter_Hostname(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg", Hostname: "web-server-01"})
	addDoc(idx, model.Document{Message: "msg", Hostname: "web-server-02"})
	addDoc(idx, model.Document{Message: "msg", Hostname: "db-server-01"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Hostname: "web-server", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("expected 2 web-server docs, got %d", r.TotalCount)
	}
}

func TestSearch_FieldFilter_Hostname_CaseInsensitive(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg", Hostname: "Web-Server-01"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Hostname: "web-server", Page: 1, PageSize: 20})
	if r.TotalCount != 1 {
		t.Errorf("hostname filter should be case-insensitive, got %d", r.TotalCount)
	}
}

// ── per-field filter: AppName ─────────────────────────────────────────────────

func TestSearch_FieldFilter_AppName(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "started", AppName: "nginx"})
	addDoc(idx, model.Document{Message: "connected", AppName: "kafka"})
	addDoc(idx, model.Document{Message: "ready", AppName: "nginx"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{AppName: "nginx", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("expected 2 nginx docs, got %d", r.TotalCount)
	}
}

// ── per-field filter: ProcID ──────────────────────────────────────────────────

func TestSearch_FieldFilter_ProcID(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg", ProcId: "1234"})
	addDoc(idx, model.Document{Message: "msg", ProcId: "5678"})
	addDoc(idx, model.Document{Message: "msg", ProcId: "1299"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{ProcID: "12", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("expected 2 docs with procid starting 12, got %d", r.TotalCount)
	}
}

// ── per-field filter: MsgID ───────────────────────────────────────────────────

func TestSearch_FieldFilter_MsgID(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg", MsgId: "ID001"})
	addDoc(idx, model.Document{Message: "msg", MsgId: "ID002"})
	addDoc(idx, model.Document{Message: "msg", MsgId: "XY999"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{MsgID: "ID", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("expected 2 docs with msgid prefix ID, got %d", r.TotalCount)
	}
}

// ── per-field filter: Groupings ───────────────────────────────────────────────

func TestSearch_FieldFilter_Groupings(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg", Groupings: "production,critical"})
	addDoc(idx, model.Document{Message: "msg", Groupings: "production,low"})
	addDoc(idx, model.Document{Message: "msg", Groupings: "staging,critical"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Groupings: "production", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("expected 2 production docs, got %d", r.TotalCount)
	}
}

// ── per-field filter: Facility ────────────────────────────────────────────────

func TestSearch_FieldFilter_Facility(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg", FacilityString: "kern"})
	addDoc(idx, model.Document{Message: "msg", FacilityString: "user"})
	addDoc(idx, model.Document{Message: "msg", FacilityString: "kernel"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Facility: "ker", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("expected 2 ker* facility docs, got %d", r.TotalCount)
	}
}

// ── per-field filter: RawMessage ──────────────────────────────────────────────

func TestSearch_FieldFilter_RawMessage(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "parsed", MessageRaw: "<13>1 2024-01-01 host app - - timeout error"})
	addDoc(idx, model.Document{Message: "parsed", MessageRaw: "<13>1 2024-01-01 host app - - started ok"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{RawMessage: "timeout", Page: 1, PageSize: 20})
	if r.TotalCount != 1 {
		t.Errorf("expected 1 raw_message timeout doc, got %d", r.TotalCount)
	}
}

// ── per-field filter: StructuredData ─────────────────────────────────────────

func TestSearch_FieldFilter_StructuredData(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "msg", StructuredData: `[meta sequenceId="1" region="us-east"]`})
	addDoc(idx, model.Document{Message: "msg", StructuredData: `[meta sequenceId="2" region="eu-west"]`})
	addDoc(idx, model.Document{Message: "msg", StructuredData: `[origin ip="1.2.3.4"]`})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{StructuredData: "region", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("expected 2 docs with region in structured_data, got %d", r.TotalCount)
	}
}

// ── combined: full-text + field filter ───────────────────────────────────────

func TestSearch_Combined_QueryAndSender(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "kafka timeout", Sender: "10.0.0.1"})
	addDoc(idx, model.Document{Message: "kafka timeout", Sender: "10.0.0.2"})
	addDoc(idx, model.Document{Message: "nginx started", Sender: "10.0.0.1"})
	idx.SetReady(true)

	// kafka query AND sender=10.0.0.1 → only 1 doc matches both
	r := search(idx, indexer.SearchQuery{
		Query: "kafka", Sender: "10.0.0.1", Page: 1, PageSize: 20,
	})
	if r.TotalCount != 1 {
		t.Errorf("combined query+sender: expected 1, got %d", r.TotalCount)
	}
}

func TestSearch_Combined_QueryAndHostname(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "error occurred", Hostname: "web-01"})
	addDoc(idx, model.Document{Message: "error occurred", Hostname: "db-01"})
	addDoc(idx, model.Document{Message: "info started", Hostname: "web-01"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{
		Query: "error", Hostname: "web-01", Page: 1, PageSize: 20,
	})
	if r.TotalCount != 1 {
		t.Errorf("combined query+hostname: expected 1, got %d", r.TotalCount)
	}
}

func TestSearch_Combined_MultipleFilters(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{
		Message: "connection refused", AppName: "kafka",
		Hostname: "broker-01", Sender: "10.1.1.1",
	})
	addDoc(idx, model.Document{
		Message: "connection refused", AppName: "nginx",
		Hostname: "broker-01", Sender: "10.1.1.2",
	})
	addDoc(idx, model.Document{
		Message: "connection refused", AppName: "kafka",
		Hostname: "proxy-01", Sender: "10.1.1.1",
	})
	idx.SetReady(true)

	// All three filters must be satisfied simultaneously
	r := search(idx, indexer.SearchQuery{
		Query:    "connection",
		AppName:  "kafka",
		Hostname: "broker-01",
		Sender:   "10.1.1.1",
		Page:     1, PageSize: 20,
	})
	if r.TotalCount != 1 {
		t.Errorf("triple-filter: expected 1 doc, got %d", r.TotalCount)
	}
}

// ── filter-only (no full-text query) ─────────────────────────────────────────

func TestSearch_FilterOnly_AppName(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{AppName: "kafka", Message: "started"})
	addDoc(idx, model.Document{AppName: "nginx", Message: "started"})
	addDoc(idx, model.Document{AppName: "kafka", Message: "connected"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{AppName: "kafka", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("filter-only AppName kafka: expected 2, got %d", r.TotalCount)
	}
}

func TestSearch_FilterOnly_AllNineFields(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{
		Sender:         "192.168.1.1",
		Hostname:       "server-prod-01",
		AppName:        "myapp",
		ProcId:         "9999",
		MsgId:          "BOOT001",
		Groupings:      "prod,critical",
		FacilityString: "daemon",
		MessageRaw:     "<134>1 raw syslog data",
		StructuredData: `[timeQuality tzKnown="1"]`,
		Message:        "application started",
	})
	addDoc(idx, model.Document{
		Sender:  "10.0.0.1",
		AppName: "other",
		Message: "unrelated",
	})
	idx.SetReady(true)

	// Filter using all 9 fields simultaneously — only the first doc should match
	r := search(idx, indexer.SearchQuery{
		Sender:         "192.168.1.1",
		Hostname:       "server-prod",
		AppName:        "myapp",
		ProcID:         "9999",
		MsgID:          "BOOT",
		Groupings:      "critical",
		Facility:       "daemon",
		RawMessage:     "syslog",
		StructuredData: "tzKnown",
		Page:           1, PageSize: 20,
	})
	if r.TotalCount != 1 {
		t.Errorf("all-nine-field filter: expected 1 doc, got %d", r.TotalCount)
	}
}

func TestSearch_FilterOnly_NoMatch(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{AppName: "nginx", Message: "started"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{AppName: "kafka", Page: 1, PageSize: 20})
	if r.TotalCount != 0 {
		t.Errorf("filter-only no match: expected 0, got %d", r.TotalCount)
	}
}

// ── LRU cache ─────────────────────────────────────────────────────────────────

func TestSearch_CacheHit(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "kafka timeout", AppName: "kafka"})
	idx.SetReady(true)

	q := indexer.SearchQuery{Query: "kafka", AppName: "kafka", Page: 1, PageSize: 20}
	r1 := search(idx, q)
	if r1.CacheHit {
		t.Error("first search should be cache miss")
	}
	r2 := search(idx, q)
	if !r2.CacheHit {
		t.Error("second identical search should be cache hit")
	}
}

func TestSearch_CacheInvalidatedOnReset(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "kafka error"})
	idx.SetReady(true)

	q := indexer.SearchQuery{Query: "kafka", Page: 1, PageSize: 20}
	search(idx, q) // populate cache
	idx.Reset()
	idx.AddDocument(model.Document{Message: "kafka error"})
	idx.SetReady(true)

	r := search(idx, q)
	if r.CacheHit {
		t.Error("cache should be cleared after reset")
	}
}

func TestSearch_DifferentFieldFilters_DifferentCacheEntries(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "err", AppName: "kafka", Hostname: "h1"})
	addDoc(idx, model.Document{Message: "err", AppName: "nginx", Hostname: "h2"})
	idx.SetReady(true)

	q1 := indexer.SearchQuery{AppName: "kafka", Page: 1, PageSize: 20}
	q2 := indexer.SearchQuery{AppName: "nginx", Page: 1, PageSize: 20}

	r1 := search(idx, q1)
	r2 := search(idx, q2)

	if r1.TotalCount != 1 || r2.TotalCount != 1 {
		t.Errorf("each filter should return 1 result, got %d and %d", r1.TotalCount, r2.TotalCount)
	}
	if r1.Documents[0].AppName == r2.Documents[0].AppName {
		t.Error("different filters should return different documents")
	}
}

// ── pagination ────────────────────────────────────────────────────────────────

func TestSearch_Pagination(t *testing.T) {
	idx := indexer.NewIndex()
	for i := 0; i < 30; i++ {
		idx.AddDocument(model.Document{
			Message:   "kafka error message",
			Namespace: "prod",
		})
	}
	idx.SetReady(true)

	cases := []struct{ page, size, wantLen int }{
		{1, 10, 10}, {2, 10, 10}, {3, 10, 10}, {4, 10, 0}, {1, 100, 30},
	}
	for _, tc := range cases {
		r := search(idx, indexer.SearchQuery{Query: "kafka", Page: tc.page, PageSize: tc.size})
		if len(r.Documents) != tc.wantLen {
			t.Errorf("page %d size %d: expected %d docs got %d",
				tc.page, tc.size, tc.wantLen, len(r.Documents))
		}
	}
}

func TestSearch_FilterOnly_Pagination(t *testing.T) {
	idx := indexer.NewIndex()
	for i := 0; i < 25; i++ {
		idx.AddDocument(model.Document{AppName: "kafka", Message: "msg"})
	}
	idx.AddDocument(model.Document{AppName: "nginx", Message: "msg"})
	idx.SetReady(true)

	r1 := search(idx, indexer.SearchQuery{AppName: "kafka", Page: 1, PageSize: 10})
	r2 := search(idx, indexer.SearchQuery{AppName: "kafka", Page: 2, PageSize: 10})
	r3 := search(idx, indexer.SearchQuery{AppName: "kafka", Page: 3, PageSize: 10})

	if r1.TotalCount != 25 {
		t.Errorf("total: expected 25 got %d", r1.TotalCount)
	}
	if len(r1.Documents) != 10 || len(r2.Documents) != 10 || len(r3.Documents) != 5 {
		t.Errorf("pagination: expected 10/10/5, got %d/%d/%d",
			len(r1.Documents), len(r2.Documents), len(r3.Documents))
	}
}

// ── stats & readiness ─────────────────────────────────────────────────────────

func TestStats(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "test document one", AppName: "svc"})
	idx.SetReady(true)

	s := idx.Stats()
	if s.TotalDocuments != 1 {
		t.Errorf("expected 1 doc, got %d", s.TotalDocuments)
	}
	if s.TotalTerms == 0 {
		t.Error("expected non-zero terms after indexing")
	}
	if !s.IndexReady {
		t.Error("expected index_ready=true")
	}
}

func TestReady_Default(t *testing.T) {
	idx := indexer.NewIndex()
	if idx.IsReady() {
		t.Error("new index should not be ready")
	}
	idx.SetReady(true)
	if !idx.IsReady() {
		t.Error("index should be ready after SetReady(true)")
	}
}

// ── timing ────────────────────────────────────────────────────────────────────

func TestSearch_TookMs(t *testing.T) {
	idx := indexer.NewIndex()
	for i := 0; i < 100; i++ {
		idx.AddDocument(model.Document{
			Message:  "kafka connection error",
			AppName:  "kafka",
			Hostname: "broker-01",
		})
	}
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{
		Query: "kafka", AppName: "kafka", Page: 1, PageSize: 20,
	})
	if r.TookMs < 0 {
		t.Error("TookMs should be non-negative")
	}
	_ = time.Now()
}

// ── fluent-bit hostname search (regression tests) ─────────────────────────────

func TestSearch_FluentBit_ExactHostname(t *testing.T) {
	// The compound hostname "fluent-bit" must be findable by full-text query
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{
		Message:  "log line from fluent-bit",
		Hostname: "fluent-bit",
		AppName:  "fluent-bit",
	})
	addDoc(idx, model.Document{
		Message:  "nginx access log",
		Hostname: "nginx-proxy",
		AppName:  "nginx",
	})
	idx.SetReady(true)

	// Full-text query should find the document
	r := search(idx, sq("fluent-bit"))
	if r.TotalCount != 1 {
		t.Errorf("full-text search 'fluent-bit': expected 1 result, got %d", r.TotalCount)
	}
}

func TestSearch_FluentBit_PartialToken_Fluent(t *testing.T) {
	// Searching "fluent" alone should also match "fluent-bit" documents
	// because the tokenizer splits compound tokens into sub-tokens
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{
		Message:  "collector started",
		Hostname: "fluent-bit",
	})
	idx.SetReady(true)

	r := search(idx, sq("fluent"))
	if r.TotalCount != 1 {
		t.Errorf("partial token 'fluent' should match hostname 'fluent-bit', got %d results", r.TotalCount)
	}
}

func TestSearch_FluentBit_PartialToken_Bit(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{
		Message:  "pipeline ready",
		Hostname: "fluent-bit",
	})
	idx.SetReady(true)

	r := search(idx, sq("bit"))
	if r.TotalCount != 1 {
		t.Errorf("partial token 'bit' should match hostname 'fluent-bit', got %d results", r.TotalCount)
	}
}

func TestSearch_FluentBit_FieldFilter_Hostname(t *testing.T) {
	// Using the hostname field filter directly
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "started", Hostname: "fluent-bit"})
	addDoc(idx, model.Document{Message: "started", Hostname: "nginx-proxy"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Hostname: "fluent-bit", Page: 1, PageSize: 20})
	if r.TotalCount != 1 {
		t.Errorf("hostname filter 'fluent-bit': expected 1, got %d", r.TotalCount)
	}
	if len(r.Documents) > 0 && r.Documents[0].Hostname != "fluent-bit" {
		t.Errorf("wrong document returned: hostname=%q", r.Documents[0].Hostname)
	}
}

func TestSearch_FluentBit_FieldFilter_Partial(t *testing.T) {
	// Substring matching in field filter: "fluent" should match "fluent-bit"
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "log", Hostname: "fluent-bit"})
	addDoc(idx, model.Document{Message: "log", Hostname: "fluent-agent"})
	addDoc(idx, model.Document{Message: "log", Hostname: "nginx"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Hostname: "fluent", Page: 1, PageSize: 20})
	if r.TotalCount != 2 {
		t.Errorf("partial hostname filter 'fluent': expected 2, got %d", r.TotalCount)
	}
}

func TestSearch_FluentBit_Combined_QueryAndHostname(t *testing.T) {
	// Full-text query + hostname field filter together
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{
		Message:  "error connection refused",
		Hostname: "fluent-bit",
	})
	addDoc(idx, model.Document{
		Message:  "error connection refused",
		Hostname: "nginx-proxy",
	})
	addDoc(idx, model.Document{
		Message:  "started successfully",
		Hostname: "fluent-bit",
	})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{
		Query: "error", Hostname: "fluent-bit", Page: 1, PageSize: 20,
	})
	if r.TotalCount != 1 {
		t.Errorf("q=error&hostname=fluent-bit: expected 1, got %d", r.TotalCount)
	}
}

func TestSearch_HyphenatedHostnames_General(t *testing.T) {
	// Ensure any hyphenated hostname is searchable
	idx := indexer.NewIndex()
	hosts := []string{"fluent-bit", "log-shipper", "kafka-broker-01", "web-server-prod"}
	for _, h := range hosts {
		addDoc(idx, model.Document{Message: "running", Hostname: h})
	}
	idx.SetReady(true)

	for _, h := range hosts {
		r := search(idx, indexer.SearchQuery{Hostname: h, Page: 1, PageSize: 20})
		if r.TotalCount != 1 {
			t.Errorf("hostname filter %q: expected 1, got %d", h, r.TotalCount)
		}
	}
}

func TestSearch_FluentBit_CaseInsensitive(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "log", Hostname: "Fluent-Bit"})
	idx.SetReady(true)

	// Field filter should match regardless of case
	r := search(idx, indexer.SearchQuery{Hostname: "fluent-bit", Page: 1, PageSize: 20})
	if r.TotalCount != 1 {
		t.Errorf("case-insensitive hostname match: expected 1, got %d", r.TotalCount)
	}
}

// ── fluent-bit hostname regression ───────────────────────────────────────────
// These tests document and guard the two bugs that were reported:
//   1. Searching ?q=fluent-bit returned no results.
//   2. Searching "fluent" alone did not match "fluent-bit" documents.
// Root cause: the old tokenizer kept "fluent-bit" as one token but did NOT
// emit the sub-tokens "fluent" and "bit", so partial queries never matched.

func TestSearch_FluentBit_FullTextQuery(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "pipeline started", Hostname: "fluent-bit", AppName: "fluent-bit"})
	addDoc(idx, model.Document{Message: "nginx access log", Hostname: "nginx-proxy", AppName: "nginx"})
	idx.SetReady(true)

	r := search(idx, sq("fluent-bit"))
	if r.TotalCount != 1 {
		t.Errorf("q=fluent-bit: expected 1 result, got %d", r.TotalCount)
	}
}

func TestSearch_FluentBit_PartialQuery_Fluent(t *testing.T) {
	// Sub-token "fluent" must match documents containing "fluent-bit"
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "collector running", Hostname: "fluent-bit"})
	addDoc(idx, model.Document{Message: "proxy running", Hostname: "nginx-proxy"})
	idx.SetReady(true)

	r := search(idx, sq("fluent"))
	if r.TotalCount != 1 {
		t.Errorf("q=fluent (partial): expected 1 match for hostname fluent-bit, got %d", r.TotalCount)
	}
}

func TestSearch_FluentBit_PartialQuery_Bit(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "pipeline ready", Hostname: "fluent-bit"})
	idx.SetReady(true)

	r := search(idx, sq("bit"))
	if r.TotalCount != 1 {
		t.Errorf("q=bit (partial): expected 1 match for hostname fluent-bit, got %d", r.TotalCount)
	}
}

func TestSearch_FluentBit_FieldFilter_Exact(t *testing.T) {
	// Hostname field filter with exact value
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "started", Hostname: "fluent-bit"})
	addDoc(idx, model.Document{Message: "started", Hostname: "nginx-proxy"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Hostname: "fluent-bit", Page: 1, PageSize: 20})
	if r.TotalCount != 1 {
		t.Errorf("hostname=fluent-bit filter: expected 1, got %d", r.TotalCount)
	}
	if len(r.Documents) > 0 && r.Documents[0].Hostname != "fluent-bit" {
		t.Errorf("wrong document returned: hostname=%q", r.Documents[0].Hostname)
	}
}

func TestSearch_FluentBit_FieldFilter_CaseInsensitive(t *testing.T) {
	idx := indexer.NewIndex()
	addDoc(idx, model.Document{Message: "log", Hostname: "Fluent-Bit"})
	idx.SetReady(true)

	r := search(idx, indexer.SearchQuery{Hostname: "fluent-bit", Page: 1, PageSize: 20})
	if r.TotalCount != 1 {
		t.Errorf("case-insensitive hostname filter: expected 1, got %d", r.TotalCount)
	}
}

func TestSearch_AnyHyphenatedHostname(t *testing.T) {
	// Regression: verify any hyphenated hostname is searchable by full value
	idx := indexer.NewIndex()
	hosts := []string{"fluent-bit", "log-shipper", "kafka-broker-01", "web-server-prod"}
	for _, h := range hosts {
		addDoc(idx, model.Document{Message: "running", Hostname: h})
	}
	idx.SetReady(true)

	for _, h := range hosts {
		r := search(idx, indexer.SearchQuery{Hostname: h, Page: 1, PageSize: 20})
		if r.TotalCount != 1 {
			t.Errorf("hostname=%q: expected 1, got %d", h, r.TotalCount)
		}
	}
}
