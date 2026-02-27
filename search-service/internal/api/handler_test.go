package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"search-service/internal/api"
	"search-service/internal/indexer"
	"search-service/internal/model"

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

func newTestRouter(idx *indexer.Index) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api.RegisterRoutes(r, idx)
	return r
}

func populatedIndex() *indexer.Index {
	idx := indexer.NewIndex()
	idx.AddDocument(model.Document{
		Message: "kafka consumer timeout error", Namespace: "prod", AppName: "kafka",
		SeverityString: "ERROR", NanoTimeStamp: time.Now().UnixNano(),
	})
	idx.AddDocument(model.Document{
		Message: "nginx started successfully", Namespace: "prod", AppName: "nginx",
		SeverityString: "INFO", NanoTimeStamp: time.Now().UnixNano(),
	})
	idx.SetReady(true)
	return idx
}

// ── /health ───────────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	r := newTestRouter(indexer.NewIndex())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

// ── /ready ────────────────────────────────────────────────────────────────────

func TestReady_NotReady(t *testing.T) {
	idx := indexer.NewIndex() // not SetReady yet
	r := newTestRouter(idx)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 got %d", w.Code)
	}
}

func TestReady_IsReady(t *testing.T) {
	idx := indexer.NewIndex()
	idx.SetReady(true)
	r := newTestRouter(idx)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

// ── /api/v1/search authentication ────────────────────────────────────────────

func TestSearch_NoAuth(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=kafka", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestSearch_InvalidToken(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=kafka", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestSearch_ValidToken_Reader(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=kafka", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"] == nil {
		t.Error("expected total_count in response")
	}
}

func TestSearch_ValidToken_Writer(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=nginx", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("writer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestSearch_ValidToken_Admin(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=kafka", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("admin"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

// ── /api/v1/search results ────────────────────────────────────────────────────

func TestSearch_Results(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=kafka&page=1&page_size=10", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	docs := resp["documents"].([]interface{})
	if len(docs) == 0 {
		t.Error("expected documents in response")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 0 {
		t.Error("empty query should return 0 results")
	}
}

// ── /api/v1/stats ─────────────────────────────────────────────────────────────

func TestStats_NoAuth(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestStats_WithAuth(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_documents"] == nil {
		t.Error("expected total_documents in stats")
	}
}

// ── /api/v1/reindex — role enforcement ───────────────────────────────────────

func TestReindex_ReaderForbidden(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reindex", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("reader should get 403 on reindex, got %d", w.Code)
	}
}

func TestReindex_WriterAllowed(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reindex", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("writer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("writer should get 202 on reindex, got %d", w.Code)
	}
}

func TestReindex_AdminAllowed(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reindex", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("admin"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("admin should get 202 on reindex, got %d", w.Code)
	}
}

// ── Pagination ────────────────────────────────────────────────────────────────

func TestSearch_Pagination(t *testing.T) {
	idx := indexer.NewIndex()
	for i := 0; i < 30; i++ {
		idx.AddDocument(model.Document{
			Message:   fmt.Sprintf("kafka error message %d", i),
			Namespace: "prod",
		})
	}
	idx.SetReady(true)
	r := newTestRouter(idx)

	for _, tc := range []struct {
		page, size, wantLen int
	}{
		{1, 10, 10},
		{2, 10, 10},
		{3, 10, 10},
		{4, 10, 0},
		{1, 100, 30},
	} {
		url := fmt.Sprintf("/api/v1/search?q=kafka&page=%d&page_size=%d", tc.page, tc.size)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("page %d size %d: expected 200 got %d", tc.page, tc.size, w.Code)
		}
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		docs := resp["documents"].([]interface{})
		if len(docs) != tc.wantLen {
			t.Errorf("page %d size %d: expected %d docs got %d", tc.page, tc.size, tc.wantLen, len(docs))
		}
	}
}

// ── Expired token ─────────────────────────────────────────────────────────────

func TestSearch_ExpiredToken(t *testing.T) {
	// Build an already-expired token.
	secret := []byte("telemetry-search-dev-secret-change-in-production")
	claims := jwt.MapClaims{
		"user_id":  "testuser",
		"username": "testuser",
		"role":     "reader",
		"exp":      time.Now().Add(-1 * time.Minute).Unix(),
		"iat":      time.Now().Add(-2 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	expiredToken, _ := token.SignedString(secret)

	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=kafka", nil)
	req.Header.Set("Authorization", "Bearer "+expiredToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d", w.Code)
	}
}
