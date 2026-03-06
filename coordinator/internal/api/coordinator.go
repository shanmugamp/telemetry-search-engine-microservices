package api

// coordinator.go — Query fan-out and result merge for WAND sharding.
//
// SHARDING STRATEGY
// Documents are distributed across N shards by a simple hash:
//   shardID = fnv32(doc.NanoTimeStamp || doc.MsgId) % numShards
// Each shard is an independent search-service pod with its own BM25+WAND index.
//
// QUERY FAN-OUT
// Every search query is sent to ALL shards in parallel (goroutine per shard).
// Each shard returns its local top-K results with BM25 scores.
//
// RESULT MERGE
// The coordinator collects per-shard results and does a final merge:
//   1. Collect all (docID, score, shardID) tuples from all shards.
//   2. Sort globally by score descending — the global top results.
//   3. Paginate and return to the client.
//
// WHY SCORES ARE COMPARABLE ACROSS SHARDS
// Each shard uses the same BM25 formula with its own local N and avgLen.
// Scores are NOT globally normalized, which means a rare term in a small
// shard scores slightly higher than the same term in a large shard.
// This is the standard trade-off in distributed BM25 (used by Elasticsearch).
// For production, global IDF can be synchronized via a periodic broadcast.

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// shardResult is the JSON shape returned by each search-service shard.
type shardResult struct {
	Documents  []map[string]interface{} `json:"documents"`
	TotalCount int                      `json:"total_count"`
	TookMs     float64                  `json:"took_ms"`
	CacheHit   bool                     `json:"cache_hit"`
}

// mergedDoc pairs a document with its score for global ranking.
type mergedDoc struct {
	doc   map[string]interface{}
	score float64
}

// Coordinator fans out queries to all shards and merges results.
type Coordinator struct {
	shards     []string // base URLs, e.g. ["http://shard-0:8082", ...]
	httpClient *http.Client
}

func NewCoordinator(shardURLs string) *Coordinator {
	urls := strings.Split(shardURLs, ",")
	for i := range urls {
		urls[i] = strings.TrimSpace(urls[i])
	}
	return &Coordinator{
		shards: urls,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// RegisterRoutes wires coordinator endpoints.
func RegisterRoutes(r *gin.Engine, shardURLs string) {
	c := NewCoordinator(shardURLs)

	r.GET("/health", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"status": "ok", "service": "coordinator", "shards": len(c.shards)})
	})

	api := r.Group("/api/v1")
	{
		api.GET("/search", c.handleSearch)
		api.GET("/stats", c.handleStats)
		api.POST("/ingest-notify", c.handleIngestNotify)
		api.POST("/reindex", c.handleReindex)
	}
}

// handleSearch fans the query to all shards, merges results, paginates.
func (co *Coordinator) handleSearch(c *gin.Context) {
	start := time.Now()

	// Build query for shards: preserve filters but request all results for global merge.
	// Strip pagination params (page, page_size) and request large page_size from each shard.
	rawQuery := stripPaginationParams(c.Request.URL.RawQuery)
	if rawQuery == "" {
		rawQuery = "page=1&page_size=1000"
	} else {
		rawQuery += "&page=1&page_size=1000"
	}
	authHeader := c.GetHeader("Authorization")

	type shardResp struct {
		result shardResult
		err    error
		tookMs float64
	}

	respCh := make(chan shardResp, len(co.shards))
	var wg sync.WaitGroup

	// Fan-out: one goroutine per shard.
	for _, shardURL := range co.shards {
		wg.Add(1)
		go func(base string) {
			defer wg.Done()
			t0 := time.Now()
			r, err := co.queryShardSearch(base, rawQuery, authHeader)
			respCh <- shardResp{result: r, err: err, tookMs: float64(time.Since(t0).Milliseconds())}
		}(shardURL)
	}

	wg.Wait()
	close(respCh)

	// Merge: collect all document+score pairs.
	var allDocs []mergedDoc
	anyCacheHit := false
	maxShardMs := 0.0

	for resp := range respCh {
		if resp.err != nil {
			slog.Warn("shard error", "err", resp.err)
			continue
		}
		anyCacheHit = anyCacheHit || resp.result.CacheHit
		if resp.result.TookMs > maxShardMs {
			maxShardMs = resp.result.TookMs
		}
		for _, doc := range resp.result.Documents {
			score := 0.0
			if s, ok := doc["_score"]; ok {
				score, _ = s.(float64)
			}
			allDocs = append(allDocs, mergedDoc{doc: doc, score: score})
		}
	}

	// Global sort by score descending.
	slices.SortFunc(allDocs, func(a, b mergedDoc) int {
		return cmp.Compare(b.score, a.score)
	})

	// Paginate.
	page, pageSize := parsePagination(c)
	lo := min((page-1)*pageSize, len(allDocs))
	hi := min(lo+pageSize, len(allDocs))
	pageDocs := make([]map[string]interface{}, hi-lo)
	for i, md := range allDocs[lo:hi] {
		pageDocs[i] = md.doc
	}

	c.JSON(http.StatusOK, gin.H{
		"documents":   pageDocs,
		"total_count": len(allDocs),
		"page":        page,
		"page_size":   pageSize,
		"took_ms":     float64(time.Since(start).Milliseconds()),
		"shard_ms":    maxShardMs,
		"cache_hit":   anyCacheHit,
		"num_shards":  len(co.shards),
	})
}

// handleStats aggregates stats from all shards.
func (co *Coordinator) handleStats(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	type shardStats struct {
		result map[string]interface{}
		err    error
	}
	ch := make(chan shardStats, len(co.shards))
	var wg sync.WaitGroup
	for _, shardURL := range co.shards {
		wg.Add(1)
		go func(base string) {
			defer wg.Done()
			r, err := co.queryShardStats(base, authHeader)
			ch <- shardStats{result: r, err: err}
		}(shardURL)
	}
	wg.Wait()
	close(ch)

	var shardList []map[string]interface{}
	totalDocs := int64(0)
	for s := range ch {
		if s.err != nil {
			continue
		}
		shardList = append(shardList, s.result)
		if d, ok := s.result["total_documents"].(float64); ok {
			totalDocs += int64(d)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"total_documents": totalDocs,
		"num_shards":      len(co.shards),
		"shards":          shardList,
	})
}

// handleIngestNotify broadcasts to all shards.
func (co *Coordinator) handleIngestNotify(c *gin.Context) {
	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	authHeader := c.GetHeader("Authorization")
	bodyBytes, _ := json.Marshal(body)

	// Determine target shard from filename hash (consistent hashing).
	filename, _ := body["filename"].(string)
	targetShard := shardIndex(filename, len(co.shards))
	shardURL := co.shards[targetShard]

	req, err := http.NewRequest("POST", shardURL+"/api/v1/ingest-notify", bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)

	resp, err := co.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(respBody, &result)
	result["shard_id"] = targetShard
	c.JSON(resp.StatusCode, result)
}

// handleReindex broadcasts reindex to all shards in parallel.
func (co *Coordinator) handleReindex(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	var wg sync.WaitGroup
	results := make([]map[string]interface{}, len(co.shards))
	for i, shardURL := range co.shards {
		wg.Add(1)
		go func(idx int, base string) {
			defer wg.Done()
			req, _ := http.NewRequest("POST", base+"/api/v1/reindex", nil)
			req.Header.Set("Authorization", authHeader)
			resp, err := co.httpClient.Do(req)
			if err != nil {
				results[idx] = map[string]interface{}{"shard_id": idx, "error": err.Error()}
				return
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			var r map[string]interface{}
			json.Unmarshal(b, &r)
			r["shard_id"] = idx
			results[idx] = r
		}(i, shardURL)
	}
	wg.Wait()
	c.JSON(http.StatusAccepted, gin.H{"shards": results})
}

// ── shard HTTP helpers ────────────────────────────────────────────────────────

func (co *Coordinator) queryShardSearch(base, rawQuery, auth string) (shardResult, error) {
	reqURL := base + "/api/v1/search"
	if rawQuery != "" {
		reqURL += "?" + rawQuery
	}
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return shardResult{}, err
	}
	req.Header.Set("Authorization", auth)

	resp, err := co.httpClient.Do(req)
	if err != nil {
		return shardResult{}, fmt.Errorf("shard %s: %w", base, err)
	}
	defer resp.Body.Close()

	var r shardResult
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return shardResult{}, fmt.Errorf("decode shard %s: %w", base, err)
	}
	return r, nil
}

func (co *Coordinator) queryShardStats(base, auth string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", base+"/api/v1/stats", nil)
	req.Header.Set("Authorization", auth)
	resp, err := co.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&r)
	return r, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// shardIndex returns a stable shard index for a filename using FNV-32.
// Same filename always routes to the same shard.
func shardIndex(filename string, numShards int) int {
	if numShards <= 1 {
		return 0
	}
	h := uint32(2166136261)
	for i := 0; i < len(filename); i++ {
		h ^= uint32(filename[i])
		h *= 16777619
	}
	return int(h) % numShards
}

func parsePagination(c *gin.Context) (page, pageSize int) {
	page = 1
	pageSize = 20
	fmt.Sscanf(c.DefaultQuery("page", "1"), "%d", &page)
	fmt.Sscanf(c.DefaultQuery("page_size", "20"), "%d", &pageSize)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// stripPaginationParams removes page and page_size from query string for shard forwarding.
// Coordinator needs to request ALL results from shards, then apply pagination globally.
func stripPaginationParams(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	params := strings.Split(rawQuery, "&")
	var filtered []string
	for _, p := range params {
		if !strings.HasPrefix(p, "page=") && !strings.HasPrefix(p, "page_size=") {
			filtered = append(filtered, p)
		}
	}
	return strings.Join(filtered, "&")
}

// Ensure url package is used (for potential future redirect handling).
var _ = url.QueryEscape
