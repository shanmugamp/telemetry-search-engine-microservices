package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"auth-service/internal/api"
	"auth-service/internal/store"
	"auth-service/pkg/jwtutil"

	"github.com/gin-gonic/gin"
)

func newTestRouter() (*gin.Engine, *store.Store) {
	gin.SetMode(gin.TestMode)
	s := store.New()
	h := api.New(s)
	r := gin.New()
	h.RegisterRoutes(r)
	return r, s
}

func TestLogin_Success(t *testing.T) {
	r, _ := newTestRouter()
	body := `{"username":"admin","password":"admin123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["access_token"] == nil || resp["refresh_token"] == nil {
		t.Fatal("expected tokens in response")
	}
}

func TestLogin_InvalidPassword(t *testing.T) {
	r, _ := newTestRouter()
	body := `{"username":"admin","password":"wrongpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	r, _ := newTestRouter()
	body := `{"username":"nobody","password":"anything"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	r, _ := newTestRouter()
	body := `{"username":"admin"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", w.Code)
	}
}

func TestRefresh_Success(t *testing.T) {
	r, s := newTestRouter()

	// First, login
	body := `{"username":"admin","password":"admin123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	refreshToken := loginResp["refresh_token"].(string)

	// Now refresh
	refreshBody, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewBuffer(refreshBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w2.Code, w2.Body.String())
	}
	var refreshResp map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &refreshResp)
	if refreshResp["access_token"] == nil {
		t.Fatal("expected new access token")
	}
	_ = s
}

func TestRefresh_TokenRotation(t *testing.T) {
	r, _ := newTestRouter()

	// Login to get refresh token
	body := `{"username":"writer","password":"writer123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	refreshToken := loginResp["refresh_token"].(string)

	// Use refresh token once — should succeed
	rb, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewBuffer(rb))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("first refresh should succeed, got %d", w2.Code)
	}

	// Use the SAME refresh token again — must fail (rotation)
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewBuffer(rb))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("second use of same refresh token should be 401, got %d", w3.Code)
	}
}

func TestValidate_ValidToken(t *testing.T) {
	r, _ := newTestRouter()
	// Generate a valid token
	pair, _ := jwtutil.GeneratePair("admin", "admin", "admin")
	body, _ := json.Marshal(map[string]string{"token": pair.AccessToken})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["valid"] != true {
		t.Fatalf("expected valid=true, got %v", resp)
	}
}

func TestValidate_InvalidToken(t *testing.T) {
	r, _ := newTestRouter()
	body := `{"token":"not.a.valid.token"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestCreateUser_Success(t *testing.T) {
	r, _ := newTestRouter()
	body := `{"username":"newuser","password":"securepass123","role":"reader"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateUser_Duplicate(t *testing.T) {
	r, _ := newTestRouter()
	body := `{"username":"admin","password":"securepass123","role":"reader"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 got %d", w.Code)
	}
}

func TestHealth(t *testing.T) {
	r, _ := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	r, _ := newTestRouter()
	// Login first
	body := `{"username":"reader","password":"reader123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var loginResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loginResp)

	// Logout
	logoutBody, _ := json.Marshal(map[string]string{"refresh_token": loginResp["refresh_token"].(string)})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", bytes.NewBuffer(logoutBody))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w2.Code)
	}
}
