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
		Message: "kafka consumer timeout error", Namespace: "prod",
		AppName: "kafka", Hostname: "broker-01", Sender: "10.0.0.1",
		ProcId: "1001", MsgId: "ID001", Groupings: "prod,critical",
		FacilityString: "daemon", MessageRaw: "<13>1 kafka timeout",
		StructuredData: `[meta region="us-east"]`,
		SeverityString: "ERROR", NanoTimeStamp: time.Now().UnixNano(),
	})
	idx.AddDocument(model.Document{
		Message: "nginx started successfully", Namespace: "prod",
		AppName: "nginx", Hostname: "proxy-01", Sender: "10.0.0.2",
		ProcId: "2002", MsgId: "ID002", Groupings: "prod,low",
		FacilityString: "user", MessageRaw: "<14>1 nginx started",
		StructuredData: `[meta region="eu-west"]`,
		SeverityString: "INFO", NanoTimeStamp: time.Now().UnixNano(),
	})
	idx.SetReady(true)
	return idx
}

func doSearch(r *gin.Engine, url string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("reader"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
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
	r := newTestRouter(indexer.NewIndex())
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

// ── auth enforcement ──────────────────────────────────────────────────────────

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
	w := doSearch(r, "/api/v1/search?q=kafka")
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

func TestSearch_ExpiredToken(t *testing.T) {
	secret := []byte("telemetry-search-dev-secret-change-in-production")
	claims := jwt.MapClaims{
		"user_id": "testuser", "username": "testuser", "role": "reader",
		"exp": time.Now().Add(-1 * time.Minute).Unix(),
		"iat": time.Now().Add(-2 * time.Minute).Unix(),
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

// ── full-text search ──────────────────────────────────────────────────────────

func TestSearch_Results(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?q=kafka&page=1&page_size=10")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	docs := resp["documents"].([]interface{})
	if len(docs) == 0 {
		t.Error("expected documents in response")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?q=")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 0 {
		t.Error("empty query with no filters should return 0 results")
	}
}

// ── per-field filter params via HTTP ─────────────────────────────────────────

func TestSearch_Filter_Sender(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?sender=10.0.0.1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("sender=10.0.0.1: expected 1 result, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_Hostname(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?hostname=broker-01")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("hostname=broker-01: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_AppName(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?app_name=nginx")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("app_name=nginx: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_ProcID(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?proc_id=1001")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("proc_id=1001: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_MsgID(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?msg_id=ID001")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("msg_id=ID001: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_Groupings(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?groupings=critical")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("groupings=critical: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_Facility(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?facility=daemon")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("facility=daemon: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_RawMessage(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?raw_message=timeout")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("raw_message=timeout: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_StructuredData(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?structured_data=us-east")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("structured_data=us-east: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_Combined_QueryAndAppName(t *testing.T) {
	r := newTestRouter(populatedIndex())
	// "kafka" matches both docs via full-text, but app_name=kafka narrows to 1
	w := doSearch(r, "/api/v1/search?q=kafka&app_name=kafka")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("q=kafka&app_name=kafka: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_AllNineFields(t *testing.T) {
	r := newTestRouter(populatedIndex())
	url := "/api/v1/search" +
		"?sender=10.0.0.1" +
		"&hostname=broker-01" +
		"&app_name=kafka" +
		"&proc_id=1001" +
		"&msg_id=ID001" +
		"&groupings=critical" +
		"&facility=daemon" +
		"&raw_message=kafka" +
		"&structured_data=us-east"
	w := doSearch(r, url)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("all-nine-fields filter: expected 1 doc, got %v", resp["total_count"])
	}
}

func TestSearch_Filter_NoMatch(t *testing.T) {
	r := newTestRouter(populatedIndex())
	w := doSearch(r, "/api/v1/search?app_name=unknown-service")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 0 {
		t.Errorf("unmatched filter: expected 0, got %v", resp["total_count"])
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
		t.Fatalf("reader should get 403, got %d", w.Code)
	}
}

func TestReindex_WriterAllowed(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reindex", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("writer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("writer should get 202, got %d", w.Code)
	}
}

func TestReindex_AdminAllowed(t *testing.T) {
	r := newTestRouter(populatedIndex())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reindex", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken("admin"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("admin should get 202, got %d", w.Code)
	}
}

// ── pagination ────────────────────────────────────────────────────────────────

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

	for _, tc := range []struct{ page, size, wantLen int }{
		{1, 10, 10}, {2, 10, 10}, {3, 10, 10}, {4, 10, 0}, {1, 100, 30},
	} {
		url := fmt.Sprintf("/api/v1/search?q=kafka&page=%d&page_size=%d", tc.page, tc.size)
		w := doSearch(r, url)
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

// ── fluent-bit hostname HTTP regression ───────────────────────────────────────

// populatedIndexWithFluentBit returns an index that contains a fluent-bit host
// alongside other hosts, used to verify the HTTP field-filter path.
func populatedIndexWithFluentBit() *indexer.Index {
	idx := indexer.NewIndex()
	idx.AddDocument(model.Document{
		Message:        "pipeline started",
		Hostname:       "fluent-bit",
		AppName:        "fluent-bit",
		Sender:         "10.1.1.1",
		SeverityString: "INFO",
		NanoTimeStamp:  time.Now().UnixNano(),
	})
	idx.AddDocument(model.Document{
		Message:        "access log",
		Hostname:       "nginx-proxy",
		AppName:        "nginx",
		Sender:         "10.1.1.2",
		SeverityString: "INFO",
		NanoTimeStamp:  time.Now().UnixNano(),
	})
	idx.AddDocument(model.Document{
		Message:        "connection error",
		Hostname:       "kafka-broker-01",
		AppName:        "kafka",
		Sender:         "10.1.1.3",
		SeverityString: "ERROR",
		NanoTimeStamp:  time.Now().UnixNano(),
	})
	idx.SetReady(true)
	return idx
}

func TestSearch_FluentBit_FullTextQuery(t *testing.T) {
	// ?q=fluent-bit must find the document via BM25 full-text
	r := newTestRouter(populatedIndexWithFluentBit())
	w := doSearch(r, "/api/v1/search?q=fluent-bit")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) < 1 {
		t.Errorf("q=fluent-bit: expected ≥1 result, got %v", resp["total_count"])
	}
}

func TestSearch_FluentBit_HostnameFilter_Exact(t *testing.T) {
	// ?hostname=fluent-bit uses the field filter path (no full-text query)
	r := newTestRouter(populatedIndexWithFluentBit())
	w := doSearch(r, "/api/v1/search?hostname=fluent-bit")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("hostname=fluent-bit: expected 1, got %v", resp["total_count"])
	}
	docs := resp["documents"].([]interface{})
	if len(docs) > 0 {
		doc := docs[0].(map[string]interface{})
		if doc["hostname"] != "fluent-bit" {
			t.Errorf("wrong document returned: hostname=%v", doc["hostname"])
		}
	}
}

func TestSearch_FluentBit_HostnameFilter_Partial(t *testing.T) {
	// ?hostname=fluent should substring-match "fluent-bit"
	r := newTestRouter(populatedIndexWithFluentBit())
	w := doSearch(r, "/api/v1/search?hostname=fluent")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("hostname=fluent (partial): expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_FluentBit_Combined_QueryAndHostname(t *testing.T) {
	// ?q=pipeline&hostname=fluent-bit — both conditions must be satisfied
	r := newTestRouter(populatedIndexWithFluentBit())
	w := doSearch(r, "/api/v1/search?q=pipeline&hostname=fluent-bit")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 1 {
		t.Errorf("q=pipeline&hostname=fluent-bit: expected 1, got %v", resp["total_count"])
	}
}

func TestSearch_FluentBit_WrongHostnameNoResults(t *testing.T) {
	// Searching for a hostname that doesn't exist must return 0
	r := newTestRouter(populatedIndexWithFluentBit())
	w := doSearch(r, "/api/v1/search?hostname=nonexistent-host")
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_count"].(float64) != 0 {
		t.Errorf("nonexistent hostname: expected 0, got %v", resp["total_count"])
	}
}
