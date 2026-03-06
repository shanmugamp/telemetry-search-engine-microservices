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

func RegisterRoutes(r *gin.Engine, idx *indexer.Index) {
	r.GET("/health", handleHealth())
	r.GET("/ready", handleReady(idx))

	api := r.Group("/api/v1")
	api.Use(middleware.RequireAuth())
	{
		api.GET("/search", handleSearch(idx))
		api.GET("/stats", handleStats(idx))
		api.POST("/reindex", middleware.RequireRole("writer"), handleReindex(idx))
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
		page, pageSize := parsePagination(c)
		username, _ := c.Get("username")

		q := indexer.SearchQuery{
			Query:          c.Query("q"),
			Sender:         c.Query("sender"),
			Hostname:       c.Query("hostname"),
			AppName:        c.Query("app_name"),
			ProcID:         c.Query("proc_id"),
			MsgID:          c.Query("msg_id"),
			Groupings:      c.Query("groupings"),
			Facility:       c.Query("facility"),
			RawMessage:     c.Query("raw_message"),
			StructuredData: c.Query("structured_data"),
			Severity:       c.Query("severity"), // NEW field
			Page:           page,
			PageSize:       pageSize,
		}

		slog.Info("search", "q", q.Query, "hostname", q.Hostname,
			"severity", q.Severity, "page", page, "user", username)

		result := idx.Search(q)
		c.JSON(http.StatusOK, result)
	}
}

func handleStats(idx *indexer.Index) gin.HandlerFunc {
	return func(c *gin.Context) { c.JSON(http.StatusOK, idx.Stats()) }
}

func handleReindex(idx *indexer.Index) gin.HandlerFunc {
	return func(c *gin.Context) {
		username, _ := c.Get("username")
		slog.Info("full reindex triggered", "user", username)

		dataDir := dataDir()
		idx.Reset()
		count := loadDir(idx, dataDir)
		// Finalize sorts all posting lists and computes WAND upper-bounds.
		idx.Finalize()
		idx.SetReady(true)

		slog.Info("reindex complete", "docs", count)
		c.JSON(http.StatusAccepted, gin.H{"message": "reindex complete", "stats": idx.Stats()})
	}
}

func handleIngestNotify(idx *indexer.Index) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Filename string `json:"filename" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "filename required"})
			return
		}

		filePath := filepath.Join(dataDir(), filepath.Base(req.Filename))
		docs, err := pq.ReadParquet(filePath)
		if err != nil {
			slog.Error("failed to index file", "file", req.Filename, "err", err)
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			return
		}

		for i := range docs {
			idx.AddDocument(docs[i])
		}
		idx.AddFile(filepath.Base(req.Filename))
		// Finalize re-sorts and re-blocks posting lists after new documents.
		idx.Finalize()
		idx.InvalidateCache()

		slog.Info("ingest-notify indexed", "file", req.Filename, "docs", len(docs))
		c.JSON(http.StatusOK, gin.H{
			"message":  "file indexed",
			"filename": filepath.Base(req.Filename),
			"docs":     len(docs),
			"stats":    idx.Stats(),
		})
	}
}

// loadDir reads all parquet files from dir and returns total docs added.
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

func dataDir() string {
	if d := os.Getenv("DATA_DIR"); d != "" {
		return d
	}
	return os.Getenv("UPLOAD_DIR")
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
