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

// RegisterRoutes wires all auth endpoints.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.health)

	v1 := r.Group("/api/v1/auth")
	{
		v1.POST("/login", h.login)
		v1.POST("/refresh", h.refresh)
		v1.POST("/logout", h.logout)
		v1.POST("/validate", h.validate) // used by gateway / other services
	}

	// Admin-only user management (protected by gateway in production)
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

	// Persist refresh token for rotation tracking.
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

	// Validate the old refresh token claims.
	claims, err := jwtutil.Parse(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}

	// Consume (rotate) the refresh token.
	rec, err := h.store.ConsumeRefresh(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token revoked or expired"})
		return
	}

	// Look up current user state (role may have changed).
	user, err := h.store.GetByID(rec.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	pair, err := jwtutil.GeneratePair(user.ID, user.Username, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}

	h.store.StoreRefresh(pair.RefreshToken, user.ID, time.Now().Add(jwtutil.RefreshTokenDuration))

	_ = claims // consumed claims used for audit if needed
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
		_, _ = h.store.ConsumeRefresh(req.RefreshToken) // invalidate
	}
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// POST /api/v1/auth/validate — called by other services/gateway to verify a token.
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

// POST /api/v1/admin/users — admin creates a new user.
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
