package indexer

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"search-service/internal/model"
)

// SearchResult is returned by Search.
type SearchResult struct {
	Documents  []model.Document `json:"documents"`
	TotalCount int              `json:"total_count"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TookMs     float64          `json:"took_ms"`
	CacheHit   bool             `json:"cache_hit"`
}

// IndexStats contains aggregate information about the index.
type IndexStats struct {
	TotalDocuments int64    `json:"total_documents"`
	TotalTerms     int      `json:"total_terms"`
	Files          []string `json:"files"`
	IndexReady     bool     `json:"index_ready"`
}

// posting stores term frequency for a single document.
type posting struct {
	freq int
}

// cacheEntry holds a cached search result with expiry.
type cacheEntry struct {
	result    SearchResult
	expiresAt time.Time
}

// Index is the core in-memory BM25 inverted index with LRU cache.
type Index struct {
	mu          sync.RWMutex
	docs        map[int64]model.Document
	inverted    map[string]map[int64]posting
	docLengths  map[int64]int
	totalLength int64
	nextID      int64
	files       []string
	ready       bool // true after initial load completes

	// Simple LRU-style cache: map[cacheKey]cacheEntry
	cacheMu sync.RWMutex
	cache   map[string]cacheEntry
}

const (
	bm25K1       = 1.2
	bm25B        = 0.75
	cacheTTL     = 30 * time.Second
	cacheMaxSize = 1000
	maxQueryLen  = 500
)

// NewIndex returns an empty, ready-to-use index.
func NewIndex() *Index {
	return &Index{
		docs:       make(map[int64]model.Document),
		inverted:   make(map[string]map[int64]posting),
		docLengths: make(map[int64]int),
		cache:      make(map[string]cacheEntry),
	}
}

// searchableText concatenates all indexed fields.
func searchableText(doc model.Document) string {
	return strings.Join([]string{
		doc.Message, doc.MessageRaw, doc.StructuredData,
		doc.Tag, doc.Sender, doc.Event, doc.Namespace,
		doc.AppName, doc.Hostname, doc.SeverityString,
		doc.FacilityString, doc.ProcId, doc.MsgId,
	}, " ")
}

// AddDocument assigns an ID and indexes doc.
func (idx *Index) AddDocument(doc model.Document) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	doc.ID = idx.nextID
	idx.nextID++
	idx.docs[doc.ID] = doc

	tokens := Tokenize(searchableText(doc))
	idx.docLengths[doc.ID] = len(tokens)
	idx.totalLength += int64(len(tokens))

	for _, token := range tokens {
		if idx.inverted[token] == nil {
			idx.inverted[token] = make(map[int64]posting)
		}
		p := idx.inverted[token][doc.ID]
		p.freq++
		idx.inverted[token][doc.ID] = p
	}
}

// AddFile records a filename as indexed.
func (idx *Index) AddFile(name string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.files = append(idx.files, name)
}

// SetReady marks the index as ready to serve traffic.
func (idx *Index) SetReady(ready bool) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.ready = ready
}

// IsReady returns whether the index has completed initial load.
func (idx *Index) IsReady() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.ready
}

// Reset clears the index and cache.
func (idx *Index) Reset() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.docs = make(map[int64]model.Document)
	idx.inverted = make(map[string]map[int64]posting)
	idx.docLengths = make(map[int64]int)
	idx.totalLength = 0
	idx.nextID = 0
	idx.files = nil
	idx.ready = false
	// Clear cache on reset
	idx.cacheMu.Lock()
	idx.cache = make(map[string]cacheEntry)
	idx.cacheMu.Unlock()
}

type scoredDoc struct {
	id    int64
	score float64
}

// Search runs a BM25 query with pagination and LRU caching.
func (idx *Index) Search(query string, page, pageSize int) SearchResult {
	// Clamp query length to prevent DoS.
	if len(query) > maxQueryLen {
		query = query[:maxQueryLen]
	}

	cacheKey := cacheKeyFor(query, page, pageSize)

	// Check cache.
	idx.cacheMu.RLock()
	if entry, ok := idx.cache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		result := entry.result
		result.CacheHit = true
		idx.cacheMu.RUnlock()
		return result
	}
	idx.cacheMu.RUnlock()

	start := time.Now()
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tokens := Tokenize(query)
	if len(tokens) == 0 {
		return SearchResult{Page: page, PageSize: pageSize}
	}

	N := float64(len(idx.docs))
	avgLen := 1.0
	if N > 0 {
		avgLen = float64(idx.totalLength) / N
	}

	scores := make(map[int64]float64, 256)
	for _, token := range tokens {
		postings, ok := idx.inverted[token]
		if !ok {
			continue
		}
		df := float64(len(postings))
		idf := math.Log((N-df+0.5)/(df+0.5) + 1)

		for docID, p := range postings {
			dl := float64(idx.docLengths[docID])
			tf := float64(p.freq)
			norm := tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*dl/avgLen))
			scores[docID] += idf * norm
		}
	}

	ranked := make([]scoredDoc, 0, len(scores))
	for id, s := range scores {
		ranked = append(ranked, scoredDoc{id, s})
	}
	slices.SortFunc(ranked, func(a, b scoredDoc) int {
		return cmp.Compare(b.score, a.score)
	})

	total := len(ranked)
	lo := min((page-1)*pageSize, total)
	hi := min(lo+pageSize, total)

	pageResults := ranked[lo:hi]
	docs := make([]model.Document, len(pageResults))
	for i := range len(pageResults) {
		docs[i] = idx.docs[pageResults[i].id]
	}

	result := SearchResult{
		Documents:  docs,
		TotalCount: total,
		Page:       page,
		PageSize:   pageSize,
		TookMs:     float64(time.Since(start).Microseconds()) / 1000.0,
		CacheHit:   false,
	}

	// Store in cache (evict if at capacity).
	idx.cacheMu.Lock()
	if len(idx.cache) >= cacheMaxSize {
		// Evict one random entry.
		for k := range idx.cache {
			delete(idx.cache, k)
			break
		}
	}
	idx.cache[cacheKey] = cacheEntry{result: result, expiresAt: time.Now().Add(cacheTTL)}
	idx.cacheMu.Unlock()

	return result
}

// Stats returns a snapshot of index metrics.
func (idx *Index) Stats() IndexStats {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return IndexStats{
		TotalDocuments: int64(len(idx.docs)),
		TotalTerms:     len(idx.inverted),
		Files:          slices.Clone(idx.files),
		IndexReady:     idx.ready,
	}
}

// InvalidateCache clears all cached search results (call after index mutation).
func (idx *Index) InvalidateCache() {
	idx.cacheMu.Lock()
	defer idx.cacheMu.Unlock()
	idx.cache = make(map[string]cacheEntry)
}

func cacheKeyFor(query string, page, pageSize int) string {
	return strings.Join([]string{query, itoa(page), itoa(pageSize)}, "|")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
