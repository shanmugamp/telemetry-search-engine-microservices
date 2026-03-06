package indexer

// WAND (Weak AND) + Block-Max BM25 + Top-K Min-Heap
//
// WHY THIS REPLACES THE NAIVE LOOP
// The old code iterated every document that contained any query token.
// For a token like "error" in 30M out of 100M docs that means 30M iterations.
//
// WAND skips documents that CANNOT beat the current top-K threshold.
// Each token stores a maxScore upper-bound (idf × max possible norm).
// If the sum of upper-bounds for a candidate document is below the threshold,
// the document is skipped without scoring — no map lookup, no arithmetic.
//
// BLOCK-MAX EXTENSION
// Posting lists are divided into blocks of 64 entries. Each block stores its
// own maxNorm. This lets WAND skip entire blocks at the posting-list level.
//
// TOP-K HEAP
// A min-heap of size K replaces the full sort. Only documents beating the
// heap minimum are inserted. The heap min becomes the WAND threshold.
// Complexity: O(n log K) instead of O(n log n). For K=1000, n=10M → ~10× faster.

import (
	"cmp"
	"container/heap"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"search-service/internal/model"
)

// ── constants ──────────────────────────────────────────────────────────────────

const (
	bm25K1       = 1.2
	bm25B        = 0.75
	cacheTTL     = 30 * time.Second
	cacheMaxSize = 1000
	maxQueryLen  = 500
	blockSize    = 64   // postings per block for Block-Max WAND
	topK         = 1000 // max candidates collected; covers 50 pages of 20
)

// ── posting structures ─────────────────────────────────────────────────────────

// postingEntry is one (docID, freq) pair. Stored sorted by docID ascending.
type postingEntry struct {
	docID int64
	freq  int32
}

// block is a slice of contiguous postingEntries plus the max norm in that block.
// maxNorm is pre-computed so block-level upper-bounds are a single multiply.
type block struct {
	entries []postingEntry
	maxNorm float32 // max tf-norm in this block (idf-independent)
}

// postingList is the full inverted list for one token.
type postingList struct {
	blocks   []block
	docFreq  int64   // total docs containing this token (= df for IDF)
	maxScore float64 // idf × global maxNorm — upper bound for WAND pivot check
}

func (pl *postingList) numEntries() int {
	n := 0
	for i := range pl.blocks {
		n += len(pl.blocks[i].entries)
	}
	return n
}

// ── cursor ────────────────────────────────────────────────────────────────────

// cursor tracks position within a postingList during WAND traversal.
type cursor struct {
	pl       *postingList
	blockIdx int
	entryIdx int
	idf      float64 // pre-computed for this token given current corpus N
}

func (c *cursor) valid() bool { return c.blockIdx < len(c.pl.blocks) }

func (c *cursor) docID() int64 {
	return c.pl.blocks[c.blockIdx].entries[c.entryIdx].docID
}

func (c *cursor) freq() int32 {
	return c.pl.blocks[c.blockIdx].entries[c.entryIdx].freq
}

// blockMaxScore returns idf × block.maxNorm for the current block.
func (c *cursor) blockMaxScore() float64 {
	if !c.valid() {
		return 0
	}
	return c.idf * float64(c.pl.blocks[c.blockIdx].maxNorm)
}

// advance moves cursor to the first position where docID >= target.
// Uses block-level last-entry check to skip whole blocks in O(1).
func (c *cursor) advance(target int64) {
	for c.blockIdx < len(c.pl.blocks) {
		blk := &c.pl.blocks[c.blockIdx]
		// Skip entire block if its last entry is before target.
		if last := blk.entries[len(blk.entries)-1].docID; last < target {
			c.blockIdx++
			c.entryIdx = 0
			continue
		}
		// Binary search within block.
		c.entryIdx = sort.Search(len(blk.entries), func(i int) bool {
			return blk.entries[i].docID >= target
		})
		if c.entryIdx < len(blk.entries) {
			return
		}
		c.blockIdx++
		c.entryIdx = 0
	}
}

// next moves to the next entry (one step forward).
func (c *cursor) next() {
	if !c.valid() {
		return
	}
	c.entryIdx++
	if c.entryIdx >= len(c.pl.blocks[c.blockIdx].entries) {
		c.blockIdx++
		c.entryIdx = 0
	}
}

// ── top-K min-heap ────────────────────────────────────────────────────────────

type scoredDoc struct {
	id    int64
	score float64
}

type minHeap []scoredDoc

func (h minHeap) Len() int            { return len(h) }
func (h minHeap) Less(i, j int) bool  { return h[i].score < h[j].score }
func (h minHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(scoredDoc)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func (h minHeap) min() float64 {
	if len(h) == 0 {
		return 0
	}
	return h[0].score
}

// ── public types ──────────────────────────────────────────────────────────────

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

// SearchQuery holds the full-text query plus optional per-field filters.
type SearchQuery struct {
	Query          string
	Sender         string
	Hostname       string
	AppName        string
	ProcID         string
	MsgID          string
	Groupings      string
	Facility       string
	RawMessage     string
	StructuredData string
	Severity       string // NEW: filter by SeverityString field
	Page           int
	PageSize       int
}

type cacheEntry struct {
	result    SearchResult
	expiresAt time.Time
}

// ── Index ─────────────────────────────────────────────────────────────────────

// Index is the WAND-accelerated BM25 inverted index.
//
// Posting lists are stored as sorted []postingEntry divided into fixed-size
// blocks. Each block carries a pre-computed maxNorm for Block-Max WAND.
// Documents are stored in full in idx.docs for hydration after scoring.
type Index struct {
	mu          sync.RWMutex
	docs        map[int64]model.Document
	docLengths  map[int64]float32 // float32 saves ~4 bytes × N vs int
	totalLength int64
	nextID      int64
	files       []string
	ready       bool

	// inverted: token → postingList (sorted by docID, divided into blocks).
	inverted map[string]*postingList
	// dirty tracks tokens whose posting lists need re-sorting + re-blocking.
	dirty map[string]struct{}

	cacheMu sync.RWMutex
	cache   map[string]cacheEntry
}

// NewIndex returns an empty, ready-to-use index.
func NewIndex() *Index {
	return &Index{
		docs:       make(map[int64]model.Document),
		docLengths: make(map[int64]float32),
		inverted:   make(map[string]*postingList),
		dirty:      make(map[string]struct{}),
		cache:      make(map[string]cacheEntry),
	}
}

// ── mutation ──────────────────────────────────────────────────────────────────

// AddDocument assigns an ID, stores the document, and appends to posting lists.
// Posting lists are NOT yet sorted — call Finalize() after bulk loads.
func (idx *Index) AddDocument(doc model.Document) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	doc.ID = idx.nextID
	idx.nextID++
	idx.docs[doc.ID] = doc

	tokens := Tokenize(searchableText(doc))
	idx.docLengths[doc.ID] = float32(len(tokens))
	idx.totalLength += int64(len(tokens))

	// Count per-token frequency within this document first to avoid
	// inflating freq by emitting one postingEntry per occurrence.
	freq := make(map[string]int32, len(tokens))
	for _, t := range tokens {
		freq[t]++
	}

	for tok, f := range freq {
		pl := idx.inverted[tok]
		if pl == nil {
			pl = &postingList{}
			idx.inverted[tok] = pl
		}
		// Append to last block (may overflow blockSize temporarily — Finalize fixes it).
		if len(pl.blocks) == 0 {
			pl.blocks = append(pl.blocks, block{entries: make([]postingEntry, 0, blockSize)})
		}
		last := &pl.blocks[len(pl.blocks)-1]
		last.entries = append(last.entries, postingEntry{docID: doc.ID, freq: f})
		pl.docFreq++
		idx.dirty[tok] = struct{}{}
	}
}

// Finalize sorts dirty posting lists by docID, re-blocks them, and
// pre-computes maxNorm per block plus the global maxScore upper-bound per token.
// Must be called after bulk AddDocument loads and before the first Search.
// It is also called automatically inside Search on any dirty token.
func (idx *Index) Finalize() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.finalizeLocked()
}

func (idx *Index) finalizeLocked() {
	if len(idx.dirty) == 0 {
		return
	}
	N := float64(len(idx.docs))
	avgLen := float64(1)
	if N > 0 {
		avgLen = float64(idx.totalLength) / N
	}
	for tok := range idx.dirty {
		idx.sortAndBlock(tok, N, avgLen)
	}
	idx.dirty = make(map[string]struct{})
}

func (idx *Index) sortAndBlock(tok string, N, avgLen float64) {
	pl := idx.inverted[tok]
	if pl == nil {
		return
	}

	// Flatten all existing blocks into one slice.
	all := make([]postingEntry, 0, pl.numEntries())
	for _, blk := range pl.blocks {
		all = append(all, blk.entries...)
	}

	// Sort by docID ascending — required for WAND cursor advancement.
	slices.SortFunc(all, func(a, b postingEntry) int {
		return cmp.Compare(a.docID, b.docID)
	})

	// Re-build blocks of exactly blockSize, compute per-block maxNorm.
	idf := idfFunc(float64(pl.docFreq), N)
	pl.blocks = pl.blocks[:0]
	var globalMaxNorm float32
	for start := 0; start < len(all); start += blockSize {
		end := start + blockSize
		if end > len(all) {
			end = len(all)
		}
		chunk := make([]postingEntry, end-start)
		copy(chunk, all[start:end])

		var blkMax float32
		for _, e := range chunk {
			dl := float64(idx.docLengths[e.docID])
			n := float32(normFunc(float64(e.freq), dl, avgLen))
			if n > blkMax {
				blkMax = n
			}
		}
		pl.blocks = append(pl.blocks, block{entries: chunk, maxNorm: blkMax})
		if blkMax > globalMaxNorm {
			globalMaxNorm = blkMax
		}
	}
	pl.maxScore = idf * float64(globalMaxNorm)
}

// ── BM25 math ─────────────────────────────────────────────────────────────────

func idfFunc(df, N float64) float64 {
	return math.Log((N-df+0.5)/(df+0.5) + 1)
}

func normFunc(tf, dl, avgLen float64) float64 {
	return tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*dl/avgLen))
}

// ── Search ────────────────────────────────────────────────────────────────────

func (idx *Index) Search(q SearchQuery) SearchResult {
	if len(q.Query) > maxQueryLen {
		q.Query = q.Query[:maxQueryLen]
	}

	cacheKey := buildCacheKey(q)

	idx.cacheMu.RLock()
	if entry, ok := idx.cache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		r := entry.result
		r.CacheHit = true
		idx.cacheMu.RUnlock()
		return r
	}
	idx.cacheMu.RUnlock()

	start := time.Now()
	idx.mu.Lock() // write-lock: finalizeLocked may mutate dirty tokens
	idx.finalizeLocked()
	idx.mu.Unlock()

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tokens := Tokenize(q.Query)
	noFullText := len(tokens) == 0
	noFilters := !hasFieldFilters(q)

	if noFullText && noFilters {
		return SearchResult{Page: q.Page, PageSize: q.PageSize}
	}

	var ranked []scoredDoc
	if noFullText {
		ranked = idx.filterScan(q)
	} else {
		N := float64(len(idx.docs))
		avgLen := float64(1)
		if N > 0 {
			avgLen = float64(idx.totalLength) / N
		}
		ranked = idx.wandBM25(tokens, q, N, avgLen)
	}

	total := len(ranked)
	lo := min((q.Page-1)*q.PageSize, total)
	hi := min(lo+q.PageSize, total)

	docs := make([]model.Document, hi-lo)
	for i, sd := range ranked[lo:hi] {
		docs[i] = idx.docs[sd.id]
	}

	result := SearchResult{
		Documents:  docs,
		TotalCount: total,
		Page:       q.Page,
		PageSize:   q.PageSize,
		TookMs:     float64(time.Since(start).Microseconds()) / 1000.0,
	}

	idx.cacheMu.Lock()
	if len(idx.cache) >= cacheMaxSize {
		for k := range idx.cache {
			delete(idx.cache, k)
			break
		}
	}
	idx.cache[cacheKey] = cacheEntry{result: result, expiresAt: time.Now().Add(cacheTTL)}
	idx.cacheMu.Unlock()

	return result
}

// ── WAND BM25 core ────────────────────────────────────────────────────────────
//
// Step-by-step:
//  1. Build one cursor per query token (skip tokens not in index).
//  2. Sort cursors by current docID ascending.
//  3. Scan right-to-left summing maxScore upper-bounds until sum > threshold.
//     The cursor at that point is the "pivot".
//  4. If all cursors before the pivot are already AT pivotDocID → score the doc.
//  5. If not → advance all pre-pivot cursors to pivotDocID and re-sort.
//  6. Block-Max check: before full scoring, verify current block's upper-bound
//     also exceeds threshold. If not, skip to next doc in that block.
//  7. Push scored doc into min-heap if score > heap.min().
//  8. Heap.min() becomes new threshold — progressively tightens the bound.
func (idx *Index) wandBM25(tokens []string, q SearchQuery, N, avgLen float64) []scoredDoc {
	// Build cursors for tokens present in the index.
	cursors := make([]*cursor, 0, len(tokens))
	for _, tok := range tokens {
		pl := idx.inverted[tok]
		if pl == nil || pl.numEntries() == 0 {
			continue
		}
		idf := idfFunc(float64(pl.docFreq), N)
		// Refresh maxScore with current N (corpus may have grown since Finalize).
		var gMax float32
		for _, blk := range pl.blocks {
			if blk.maxNorm > gMax {
				gMax = blk.maxNorm
			}
		}
		pl.maxScore = idf * float64(gMax)
		cursors = append(cursors, &cursor{pl: pl, idf: idf})
	}
	if len(cursors) == 0 {
		return nil
	}

	h := &minHeap{}
	heap.Init(h)
	threshold := 0.0

	// sortActive sorts valid cursors by docID ascending; drops exhausted ones.
	sortActive := func() {
		active := cursors[:0]
		for _, c := range cursors {
			if c.valid() {
				active = append(active, c)
			}
		}
		cursors = active
		slices.SortFunc(cursors, func(a, b *cursor) int {
			return cmp.Compare(a.docID(), b.docID())
		})
	}
	sortActive()

	for len(cursors) > 0 {
		// ── Find pivot: right-to-left prefix sum of maxScores ──────────────
		sum := 0.0
		pivot := -1
		for i := len(cursors) - 1; i >= 0; i-- {
			sum += cursors[i].pl.maxScore
			if sum > threshold {
				pivot = i
				break
			}
		}
		if pivot < 0 {
			break // no document can beat threshold
		}

		pivotDoc := cursors[pivot].docID()

		// ── Check all cursors before pivot are at pivotDoc ─────────────────
		allAtPivot := true
		for i := 0; i < pivot; i++ {
			if cursors[i].docID() != pivotDoc {
				allAtPivot = false
				break
			}
		}

		if !allAtPivot {
			// Advance pre-pivot cursors to pivotDoc and re-sort.
			for i := 0; i < pivot; i++ {
				cursors[i].advance(pivotDoc)
			}
			sortActive()
			continue
		}

		// ── Block-Max check ────────────────────────────────────────────────
		blockSum := 0.0
		for _, c := range cursors {
			if c.valid() && c.docID() == pivotDoc {
				blockSum += c.blockMaxScore()
			}
		}
		if blockSum <= threshold {
			// Current block cannot beat threshold — skip pivotDoc.
			for _, c := range cursors {
				if c.valid() && c.docID() == pivotDoc {
					c.advance(pivotDoc + 1)
				}
			}
			sortActive()
			continue
		}

		// ── Full BM25 score ────────────────────────────────────────────────
		doc, exists := idx.docs[pivotDoc]
		if !exists {
			for _, c := range cursors {
				if c.valid() && c.docID() == pivotDoc {
					c.next()
				}
			}
			sortActive()
			continue
		}

		// Apply field filters before the multiply — avoids wasted arithmetic.
		if !fieldFiltersMatch(doc, q) {
			for _, c := range cursors {
				if c.valid() && c.docID() == pivotDoc {
					c.next()
				}
			}
			sortActive()
			continue
		}

		dl := float64(idx.docLengths[pivotDoc])
		score := 0.0
		for _, c := range cursors {
			if c.valid() && c.docID() == pivotDoc {
				score += c.idf * normFunc(float64(c.freq()), dl, avgLen)
				c.next()
			}
		}

		// ── Update min-heap ────────────────────────────────────────────────
		if h.Len() < topK || score > threshold {
			heap.Push(h, scoredDoc{id: pivotDoc, score: score})
			if h.Len() > topK {
				heap.Pop(h)
			}
			if h.Len() == topK {
				threshold = h.min()
			}
		}
		sortActive()
	}

	// Drain heap into a slice and sort descending.
	results := make([]scoredDoc, h.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(h).(scoredDoc)
	}
	// Results come out of heap in ascending order; reverse for descending.
	slices.SortFunc(results, func(a, b scoredDoc) int {
		return cmp.Compare(b.score, a.score)
	})
	return results
}

// filterScan handles queries with field filters but no full-text query.
func (idx *Index) filterScan(q SearchQuery) []scoredDoc {
	ranked := make([]scoredDoc, 0, 64)
	for id, doc := range idx.docs {
		if fieldFiltersMatch(doc, q) {
			ranked = append(ranked, scoredDoc{id: id, score: float64(-id)})
		}
	}
	slices.SortFunc(ranked, func(a, b scoredDoc) int {
		return cmp.Compare(a.score, b.score)
	})
	return ranked
}

// ── field helpers ─────────────────────────────────────────────────────────────

func fieldFiltersMatch(doc model.Document, q SearchQuery) bool {
	check := func(field, filter string) bool {
		if filter == "" {
			return true
		}
		return strings.Contains(strings.ToLower(field), strings.ToLower(filter))
	}
	return check(doc.Sender, q.Sender) &&
		check(doc.Hostname, q.Hostname) &&
		check(doc.AppName, q.AppName) &&
		check(doc.ProcId, q.ProcID) &&
		check(doc.MsgId, q.MsgID) &&
		check(doc.Groupings, q.Groupings) &&
		check(doc.FacilityString, q.Facility) &&
		check(doc.MessageRaw, q.RawMessage) &&
		check(doc.StructuredData, q.StructuredData) &&
		check(doc.SeverityString, q.Severity)
}

func hasFieldFilters(q SearchQuery) bool {
	return q.Sender != "" || q.Hostname != "" || q.AppName != "" ||
		q.ProcID != "" || q.MsgID != "" || q.Groupings != "" ||
		q.Facility != "" || q.RawMessage != "" || q.StructuredData != "" ||
		q.Severity != ""
}

func searchableText(doc model.Document) string {
	return strings.Join([]string{
		doc.Message, doc.MessageRaw, doc.StructuredData, doc.Tag,
		doc.Sender, doc.Hostname, doc.AppName, doc.ProcId, doc.MsgId,
		doc.Groupings, doc.FacilityString, doc.SeverityString,
		doc.Event, doc.Namespace,
	}, " ")
}

// ── index management ──────────────────────────────────────────────────────────

func (idx *Index) AddFile(name string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.files = append(idx.files, name)
}

func (idx *Index) SetReady(v bool) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.ready = v
}

func (idx *Index) IsReady() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.ready
}

func (idx *Index) Reset() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.docs = make(map[int64]model.Document)
	idx.docLengths = make(map[int64]float32)
	idx.inverted = make(map[string]*postingList)
	idx.dirty = make(map[string]struct{})
	idx.totalLength = 0
	idx.nextID = 0
	idx.files = nil
	idx.ready = false
	idx.cacheMu.Lock()
	idx.cache = make(map[string]cacheEntry)
	idx.cacheMu.Unlock()
}

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

func (idx *Index) InvalidateCache() {
	idx.cacheMu.Lock()
	defer idx.cacheMu.Unlock()
	idx.cache = make(map[string]cacheEntry)
}

// ── cache key ─────────────────────────────────────────────────────────────────

func buildCacheKey(q SearchQuery) string {
	return strings.Join([]string{
		q.Query, q.Sender, q.Hostname, q.AppName,
		q.ProcID, q.MsgID, q.Groupings, q.Facility,
		q.RawMessage, q.StructuredData, q.Severity,
		itoa(q.Page), itoa(q.PageSize),
	}, "|")
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
