package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"search-service/internal/indexer"
	"search-service/internal/middleware"
	pq "search-service/internal/parquet"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes wires all search endpoints with JWT protection.
func RegisterRoutes(r *gin.Engine, idx *indexer.Index) {
	r.GET("/health", handleHealth())
	r.GET("/ready", handleReady(idx))

	// All API routes require authentication.
	api := r.Group("/api/v1")
	api.Use(middleware.RequireAuth())
	{
		// reader role: can search and view stats
		api.GET("/search", handleSearch(idx))
		api.GET("/stats", handleStats(idx))

		// writer role: can trigger reindex or notify of new files
		api.POST("/reindex", middleware.RequireRole("writer"), handleReindex(idx))

		// Called by ingest-service after a successful upload to index the new file
		api.POST("/ingest-notify", middleware.RequireRole("writer"), handleIngestNotify(idx))
	}
}

func handleHealth() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "search-service"})
	}
}

func handleReady(idx *indexer.Index) gin.HandlerFunc {
	return func(c *gin.Context) {
		if idx.IsReady() {
			c.JSON(http.StatusOK, gin.H{"status": "ready"})
		} else {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "loading"})
		}
	}
}

func handleSearch(idx *indexer.Index) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Query("q")
		page, pageSize := parsePagination(c)
		username, _ := c.Get("username")
		slog.Info("search request", "query", query, "page", page, "user", username)
		result := idx.Search(query, page, pageSize)
		c.JSON(http.StatusOK, result)
	}
}

func handleStats(idx *indexer.Index) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, idx.Stats())
	}
}

// handleReindex does a full reset + reload of all parquet files from DATA_DIR.
func handleReindex(idx *indexer.Index) gin.HandlerFunc {
	return func(c *gin.Context) {
		username, _ := c.Get("username")
		slog.Info("full reindex triggered", "user", username)

		dataDir := os.Getenv("DATA_DIR")
		if dataDir == "" {
			dataDir = os.Getenv("UPLOAD_DIR")
		}

		idx.Reset()
		count := loadDir(idx, dataDir)
		idx.SetReady(true)

		slog.Info("reindex complete", "docs", count, "stats", idx.Stats())
		c.JSON(http.StatusOK, gin.H{
			"message": "reindex complete",
			"stats":   idx.Stats(),
		})
	}
}

// handleIngestNotify indexes a single newly uploaded file without a full reset.
// Called by ingest-service after a successful file save.
func handleIngestNotify(idx *indexer.Index) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Filename string `json:"filename" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "filename required"})
			return
		}

		dataDir := os.Getenv("DATA_DIR")
		if dataDir == "" {
			dataDir = os.Getenv("UPLOAD_DIR")
		}

		filePath := filepath.Join(dataDir, filepath.Base(req.Filename))
		docs, err := pq.ReadParquet(filePath)
		if err != nil {
			slog.Error("failed to index new file", "file", req.Filename, "err", err)
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			return
		}

		for i := range docs {
			idx.AddDocument(docs[i])
		}
		idx.AddFile(filepath.Base(req.Filename))
		idx.InvalidateCache()

		slog.Info("ingest-notify: file indexed", "file", req.Filename, "docs", len(docs))
		c.JSON(http.StatusOK, gin.H{
			"message":  "file indexed",
			"filename": filepath.Base(req.Filename),
			"docs":     len(docs),
			"stats":    idx.Stats(),
		})
	}
}

// loadDir reads all parquet files from dir into the index and returns total docs added.
func loadDir(idx *indexer.Index, dir string) int {
	if dir == "" {
		return 0
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil {
		slog.Warn("glob failed", "dir", dir, "err", err)
		return 0
	}
	total := 0
	for _, f := range files {
		docs, err := pq.ReadParquet(f)
		if err != nil {
			slog.Warn("skipping file", "path", f, "err", err)
			continue
		}
		for i := range docs {
			idx.AddDocument(docs[i])
		}
		idx.AddFile(filepath.Base(f))
		total += len(docs)
		slog.Info("indexed file", "file", filepath.Base(f), "docs", len(docs))
	}
	return total
}

func parsePagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	pageSize = max(1, min(pageSize, 100))
	return
}
