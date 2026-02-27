package api

import (
	"log/slog"
	"net/http"
	"time"

	"auth-service/internal/store"
	"auth-service/pkg/jwtutil"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	store *store.Store
}

func New(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.health)

	v1 := r.Group("/api/v1/auth")
	{
		v1.POST("/login", h.login)
		v1.POST("/refresh", h.refresh)
		v1.POST("/logout", h.logout)
		v1.POST("/validate", h.validate)
	}

	admin := r.Group("/api/v1/admin")
	{
		admin.POST("/users", h.createUser)
		admin.GET("/users", h.listUsers)
	}
}

// POST /api/v1/auth/login
func (h *Handler) login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	user, err := h.store.Authenticate(req.Username, req.Password)
	if err != nil {
		slog.Warn("login failed", "username", req.Username, "remote", c.ClientIP())
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	pair, err := jwtutil.GeneratePair(user.ID, user.Username, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}

	h.store.StoreRefresh(pair.RefreshToken, user.ID, time.Now().Add(jwtutil.RefreshTokenDuration))

	slog.Info("login success", "username", user.Username, "role", user.Role)
	c.JSON(http.StatusOK, gin.H{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    pair.ExpiresIn,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

// POST /api/v1/auth/refresh
func (h *Handler) refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token required"})
		return
	}

	// Step 1: Consume (rotate) the token FIRST — single-use enforcement.
	// If not found in store, it was already used or was never issued → reject immediately.
	// This check MUST happen before JWT signature validation so that even a
	// cryptographically valid but already-consumed token is rejected.
	rec, err := h.store.ConsumeRefresh(req.RefreshToken)
	if err != nil {
		slog.Warn("refresh rejected - token not in store", "err", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token revoked or expired"})
		return
	}

	// Step 2: Validate JWT signature and expiry AFTER confirming it's in the store.
	// ParseRefresh also enforces token_type == "refresh" so access tokens cannot be used here.
	if _, err := jwtutil.ParseRefresh(req.RefreshToken); err != nil {
		slog.Warn("refresh rejected - invalid JWT", "err", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}

	// Step 3: Look up current user (role may have changed since token was issued).
	user, err := h.store.GetByID(rec.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	// Step 4: Issue new token pair and store the new refresh token.
	pair, err := jwtutil.GeneratePair(user.ID, user.Username, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}

	h.store.StoreRefresh(pair.RefreshToken, user.ID, time.Now().Add(jwtutil.RefreshTokenDuration))

	c.JSON(http.StatusOK, gin.H{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    pair.ExpiresIn,
	})
}

// POST /api/v1/auth/logout
func (h *Handler) logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.RefreshToken != "" {
		_, _ = h.store.ConsumeRefresh(req.RefreshToken)
	}
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// POST /api/v1/auth/validate
func (h *Handler) validate(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token required"})
		return
	}

	claims, err := jwtutil.Parse(req.Token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"valid": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":    true,
		"user_id":  claims.UserID,
		"username": claims.Username,
		"role":     claims.Role,
	})
}

// POST /api/v1/admin/users
func (h *Handler) createUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required,min=8"`
		Role     string `json:"role" binding:"required,oneof=admin writer reader"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.store.CreateUser(req.Username, req.Password, req.Role)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":         user.ID,
		"username":   user.Username,
		"role":       user.Role,
		"created_at": user.CreatedAt,
	})
}

// GET /api/v1/admin/users
func (h *Handler) listUsers(c *gin.Context) {
	users := h.store.ListUsers()
	out := make([]gin.H, len(users))
	for i, u := range users {
		out[i] = gin.H{
			"id":         u.ID,
			"username":   u.Username,
			"role":       u.Role,
			"active":     u.Active,
			"created_at": u.CreatedAt,
		}
	}
	c.JSON(http.StatusOK, gin.H{"users": out, "count": len(out)})
}

func (h *Handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "auth-service"})
}
