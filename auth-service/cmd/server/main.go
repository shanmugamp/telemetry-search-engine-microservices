package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"auth-service/internal/api"
	"auth-service/internal/store"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	s := store.New()
	h := api.New(s)

	gin.SetMode(envOr("GIN_MODE", "release"))
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{envOr("ALLOWED_ORIGIN", "http://localhost:3000")},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	// Request ID middleware
	r.Use(func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = generateID()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	})

	h.RegisterRoutes(r)

	port := envOr("PORT", "8081")
	slog.Info("auth-service starting", "port", port)
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

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
