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

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doJSONBytes(r *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func loginAs(t *testing.T, r *gin.Engine, username, password string) (accessToken, refreshToken string) {
	t.Helper()
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["access_token"].(string), resp["refresh_token"].(string)
}

func TestLogin_Success(t *testing.T) {
	r, _ := newTestRouter()
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"admin123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["access_token"] == nil || resp["refresh_token"] == nil {
		t.Fatal("expected tokens in response")
	}
}

func TestLogin_InvalidPassword(t *testing.T) {
	r, _ := newTestRouter()
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"wrongpassword"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	r, _ := newTestRouter()
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login", `{"username":"nobody","password":"anything"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	r, _ := newTestRouter()
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login", `{"username":"admin"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", w.Code)
	}
}

func TestRefresh_Success(t *testing.T) {
	r, _ := newTestRouter()
	_, refreshToken := loginAs(t, r, "admin", "admin123")

	// Use the refresh token — each call gets a fresh buffer
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	w := doJSONBytes(r, http.MethodPost, "/api/v1/auth/refresh", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["access_token"] == nil {
		t.Fatal("expected new access token")
	}
}

func TestRefresh_TokenRotation(t *testing.T) {
	r, _ := newTestRouter()
	_, refreshToken := loginAs(t, r, "writer", "writer123")

	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})

	// First use — must succeed
	w1 := doJSONBytes(r, http.MethodPost, "/api/v1/auth/refresh", body)
	if w1.Code != http.StatusOK {
		t.Fatalf("first refresh should succeed, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second use of the SAME token — must fail (single-use rotation)
	// IMPORTANT: create a fresh buffer; the previous one was consumed by the HTTP read
	body2, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	w2 := doJSONBytes(r, http.MethodPost, "/api/v1/auth/refresh", body2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("second use of same refresh token should be 401, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestRefresh_InvalidToken(t *testing.T) {
	r, _ := newTestRouter()
	w := doJSON(r, http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token":"invalid.token.here"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestValidate_ValidToken(t *testing.T) {
	r, _ := newTestRouter()
	pair, _ := jwtutil.GeneratePair("admin", "admin", "admin")
	body, _ := json.Marshal(map[string]string{"token": pair.AccessToken})
	w := doJSONBytes(r, http.MethodPost, "/api/v1/auth/validate", body)
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
	w := doJSON(r, http.MethodPost, "/api/v1/auth/validate", `{"token":"not.a.valid.token"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	r, _ := newTestRouter()
	// Manually crafted expired token (will fail signature or expiry check)
	expiredToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImFkbWluIiwiZXhwIjoxfQ.invalid"
	body, _ := json.Marshal(map[string]string{"token": expiredToken})
	w := doJSONBytes(r, http.MethodPost, "/api/v1/auth/validate", body)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	r, _ := newTestRouter()
	_, refreshToken := loginAs(t, r, "reader", "reader123")

	logoutBody, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	w := doJSONBytes(r, http.MethodPost, "/api/v1/auth/logout", logoutBody)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}

	// After logout, refresh must fail
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	w2 := doJSONBytes(r, http.MethodPost, "/api/v1/auth/refresh", body)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout should be 401, got %d", w2.Code)
	}
}

func TestCreateUser_Success(t *testing.T) {
	r, _ := newTestRouter()
	w := doJSON(r, http.MethodPost, "/api/v1/admin/users",
		`{"username":"newuser","password":"securepass123","role":"reader"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateUser_Duplicate(t *testing.T) {
	r, _ := newTestRouter()
	w := doJSON(r, http.MethodPost, "/api/v1/admin/users",
		`{"username":"admin","password":"securepass123","role":"reader"}`)
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
