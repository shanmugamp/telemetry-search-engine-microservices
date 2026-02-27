package main

import (
	"log/slog"
	"os"

	"ingest-service/internal/api"
	"ingest-service/internal/middleware"
	"ingest-service/internal/worker"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	uploadDir := envOr("UPLOAD_DIR", "/app/uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		slog.Error("failed to create upload dir", "err", err)
		os.Exit(1)
	}

	// URL of the search-service for ingest-notify calls (internal Docker network)
	searchServiceURL := envOr("SEARCH_SERVICE_URL", "http://search-service:8082")

	q := worker.New(4)
	h := api.New(q, uploadDir, searchServiceURL)

	gin.SetMode(envOr("GIN_MODE", "release"))
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{envOr("ALLOWED_ORIGIN", "http://localhost:3000")},
		AllowMethods:     []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"Content-Length", "X-Request-ID"},
		AllowCredentials: true,
	}))
	r.MaxMultipartMemory = 512 << 20 // 512 MB

	h.RegisterRoutes(r)

	port := envOr("PORT", "8083")
	slog.Info("ingest-service starting", "port", port,
		"upload_dir", uploadDir,
		"search_service_url", searchServiceURL)

	if err := r.Run(":" + port); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
