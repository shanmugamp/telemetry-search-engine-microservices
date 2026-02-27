package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"search-service/internal/api"
	"search-service/internal/indexer"
	"search-service/internal/middleware"
	pq "search-service/internal/parquet"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	idx := indexer.NewIndex()

	// Scan DATA_DIR and UPLOAD_DIR at startup.
	dirs := uniqueDirs(os.Getenv("DATA_DIR"), os.Getenv("UPLOAD_DIR"))
	if len(dirs) == 0 {
		slog.Warn("no DATA_DIR or UPLOAD_DIR set")
	}
	for _, dir := range dirs {
		loadDir(idx, dir)
	}

	// Mark index ready AFTER loading — readiness probe won't pass until here.
	idx.SetReady(true)
	slog.Info("index ready", "documents", idx.Stats().TotalDocuments, "terms", idx.Stats().TotalTerms)

	gin.SetMode(envOr("GIN_MODE", "release"))
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{envOr("ALLOWED_ORIGIN", "http://localhost:3000")},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"Content-Length", "X-Request-ID"},
		AllowCredentials: true,
	}))

	api.RegisterRoutes(r, idx)

	port := envOr("PORT", "8082")
	slog.Info("search-service starting", "port", port)
	if err := r.Run(":" + port); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func loadDir(idx *indexer.Index, dir string) {
	files, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil {
		slog.Warn("glob failed", "dir", dir, "err", err)
		return
	}
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
		slog.Info("indexed at startup", "file", filepath.Base(f), "docs", len(docs))
	}
}

func uniqueDirs(dirs ...string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
