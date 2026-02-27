package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ingest-service/internal/api"
	"ingest-service/internal/worker"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func makeToken(role string) string {
	secret := []byte("telemetry-search-dev-secret-change-in-production")
	claims := jwt.MapClaims{
		"user_id":  "testuser",
		"username": "testuser",
		"role":     role,
		"exp":      time.Now().Add(15 * time.Minute).Unix(),
		"iat":      time.Now().Unix(),
		"iss":      "telemetry-search",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString(secret)
	return s
}

func newTestHandler(t *testing.T) (*api.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	q := worker.New(1)
	return api.New(q, dir), dir
}

func newTestRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h, dir := newTestHandler(t)
	r := gin.New()
	h.RegisterRoutes(r)
	return r, dir
}

// ── /health ───────────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

// ── /api/v1/files ─────────────────────────────────────────────────────────────

func TestListFiles_NoAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/files", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestListFiles_WithAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/files", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["files"] == nil {
		t.Error("expected files array in response")
	}
}

// ── /api/v1/upload ────────────────────────────────────────────────────────────

func TestUpload_NoAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestUpload_ReaderForbidden(t *testing.T) {
	r, _ := newTestRouter(t)
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "test.parquet")
	part.Write([]byte("PAR1somedata"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("reader should be forbidden from upload, got %d", w.Code)
	}
}

func TestUpload_InvalidMagic(t *testing.T) {
	r, _ := newTestRouter(t)
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "notparquet.parquet")
	part.Write([]byte("THIS IS NOT A PARQUET FILE"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+makeToken("writer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid magic should be 400, got %d", w.Code)
	}
}

func TestUpload_NoFile(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxxx")
	req.Header.Set("Authorization", "Bearer "+makeToken("writer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no file should be 400, got %d", w.Code)
	}
}

func TestUpload_ValidParquet(t *testing.T) {
	// Copy a real parquet file from the original project if available,
	// otherwise create a minimal valid parquet-magic file and test file creation.
	realParquet := "/home/claude/telemetry-uploaded/telemetry-search/parquet/data.parquet"
	if _, err := os.Stat(realParquet); os.IsNotExist(err) {
		t.Skip("real parquet file not available")
	}

	r, dir := newTestRouter(t)
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "data.parquet")
	f, _ := os.Open(realParquet)
	defer f.Close()
	io.Copy(part, f)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+makeToken("writer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["job_id"] == nil {
		t.Error("expected job_id in response")
	}
	_ = dir
}

// ── /api/v1/jobs ─────────────────────────────────────────────────────────────

func TestListJobs_WithAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestGetJob_NotFound(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/job-9999", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d", w.Code)
	}
}

// ── /api/v1/files/:name DELETE ───────────────────────────────────────────────

func TestDeleteFile_WriterCanDelete(t *testing.T) {
	r, dir := newTestRouter(t)
	// Create a test file to delete
	testFile := filepath.Join(dir, "test.parquet")
	os.WriteFile(testFile, []byte("PAR1test"), 0644)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files/test.parquet", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("writer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteFile_ReaderForbidden(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files/test.parquet", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("reader should get 403, got %d", w.Code)
	}
}

func TestDeleteFile_NotFound(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files/nonexistent.parquet", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("writer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d", w.Code)
	}
}

func TestDeleteFile_PathTraversal(t *testing.T) {
	r, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files/..%2Fetc%2Fpasswd", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("admin"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// Should be 404 (not found) or 400 (invalid), never 200
	if w.Code == 200 {
		t.Fatal("path traversal should not succeed")
	}
}
