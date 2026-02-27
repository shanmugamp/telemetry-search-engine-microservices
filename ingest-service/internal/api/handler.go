package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ingest-service/internal/middleware"
	"ingest-service/internal/worker"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	queue          *worker.Queue
	uploadDir      string
	searchServiceURL string
}

func New(q *worker.Queue, uploadDir, searchServiceURL string) *Handler {
	return &Handler{
		queue:            q,
		uploadDir:        uploadDir,
		searchServiceURL: searchServiceURL,
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.health)
	r.GET("/ready", h.ready)

	api := r.Group("/api/v1")
	api.Use(middleware.RequireAuth())
	{
		api.POST("/upload", middleware.RequireRole("writer"), h.upload)
		api.GET("/files", h.listFiles)
		api.GET("/jobs", h.listJobs)
		api.GET("/jobs/:id", h.getJob)
		api.DELETE("/files/:name", middleware.RequireRole("writer"), h.deleteFile)
	}
}

// POST /api/v1/upload
func (h *Handler) upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no file provided"})
		return
	}

	// Validate PAR1 magic bytes
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read file"})
		return
	}
	magic := make([]byte, 4)
	if _, err := io.ReadFull(src, magic); err != nil || string(magic) != "PAR1" {
		src.Close()
		c.JSON(http.StatusBadRequest, gin.H{"error": "not a valid Parquet file (missing PAR1 magic)"})
		return
	}
	src.Close()

	saveName := ensureParquetExt(filepath.Base(file.Filename))
	dst := filepath.Join(h.uploadDir, saveName)

	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("save failed: %v", err)})
		return
	}

	username, _ := c.Get("username")
	usernameStr := fmt.Sprintf("%v", username)

	// Get the Bearer token from the request to forward to search-service
	authHeader := c.GetHeader("Authorization")

	jobID := h.queue.Submit(saveName, dst, usernameStr, func(job *worker.Job) {
		slog.Info("ingest job complete", "job_id", job.ID, "docs", job.DocsIndexed)
		// Notify search-service to index the new file
		h.notifySearchService(saveName, authHeader)
	})

	slog.Info("upload received", "file", saveName, "job", jobID, "user", usernameStr)
	c.JSON(http.StatusAccepted, gin.H{
		"message":  fmt.Sprintf("file %s accepted for ingestion", saveName),
		"job_id":   jobID,
		"filename": saveName,
	})
}

// notifySearchService calls the search-service /api/v1/ingest-notify endpoint
// so it indexes the newly uploaded file without needing a restart.
func (h *Handler) notifySearchService(filename, authHeader string) {
	if h.searchServiceURL == "" {
		slog.Warn("SEARCH_SERVICE_URL not set — search-service will not be notified of new file")
		return
	}

	body, _ := json.Marshal(map[string]string{"filename": filename})
	url := strings.TrimRight(h.searchServiceURL, "/") + "/api/v1/ingest-notify"

	// Retry up to 3 times with backoff — search-service may still be loading
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
		if err != nil {
			slog.Error("notify: failed to build request", "err", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", authHeader)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("notify: search-service unreachable", "attempt", attempt, "err", err)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			slog.Info("notify: search-service indexed new file", "file", filename)
			return
		}
		respBody, _ := io.ReadAll(resp.Body)
		slog.Warn("notify: search-service returned error", "status", resp.StatusCode, "body", string(respBody), "attempt", attempt)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	slog.Error("notify: failed to notify search-service after 3 attempts", "file", filename)
}

// GET /api/v1/files
func (h *Handler) listFiles(c *gin.Context) {
	entries, err := os.ReadDir(h.uploadDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type fileInfo struct {
		Name      string `json:"name"`
		SizeBytes int64  `json:"size_bytes"`
		ModTime   string `json:"mod_time"`
	}
	files := make([]fileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || e.Name() == ".gitkeep" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			Name:      e.Name(),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().UTC().Format("2006-01-02 15:04:05 UTC"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"files": files, "count": len(files)})
}

// DELETE /api/v1/files/:name
func (h *Handler) deleteFile(c *gin.Context) {
	name := filepath.Base(c.Param("name"))
	if name == ".gitkeep" || name == "." || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}
	for _, ch := range name {
		if !isAllowedFilenameChar(ch) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename characters"})
			return
		}
	}
	target := filepath.Join(h.uploadDir, name)
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	username, _ := c.Get("username")
	slog.Info("file deleted", "file", name, "user", username)
	c.JSON(http.StatusOK, gin.H{"deleted": name})
}

// GET /api/v1/jobs
func (h *Handler) listJobs(c *gin.Context) {
	jobs := h.queue.ListJobs()
	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "count": len(jobs)})
}

// GET /api/v1/jobs/:id
func (h *Handler) getJob(c *gin.Context) {
	id := c.Param("id")
	job, ok := h.queue.GetJob(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *Handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "ingest-service"})
}

func (h *Handler) ready(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func ensureParquetExt(name string) string {
	if strings.ToLower(filepath.Ext(name)) == ".parquet" {
		return name
	}
	return name + ".parquet"
}

func isAllowedFilenameChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '-' || ch == '_' || ch == '.' || ch == ' '
}
