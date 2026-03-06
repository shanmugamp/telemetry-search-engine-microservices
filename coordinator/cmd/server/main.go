package main

import (
	"log/slog"
	"os"

	"coordinator/internal/api"
	"coordinator/internal/middleware"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// SHARD_URLS: comma-separated list of internal shard base URLs.
	// e.g. "http://shard-0:8082,http://shard-1:8082,http://shard-2:8082"
	shardURLs := envOr("SHARD_URLS", "http://search-service:8082")

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

	api.RegisterRoutes(r, shardURLs)

	port := envOr("PORT", "8090")
	slog.Info("coordinator starting", "port", port, "shard_urls", shardURLs)
	if err := r.Run(":" + port); err != nil {
		slog.Error("coordinator error", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
