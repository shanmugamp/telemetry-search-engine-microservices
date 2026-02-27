package middleware

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Claims mirrors jwtutil.Claims — each service parses tokens independently.
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func jwtSecret() []byte {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		s = "telemetry-search-dev-secret-change-in-production"
	}
	return []byte(s)
}

// ParseToken validates a JWT string and returns claims.
func ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret(), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// RequireAuth extracts and validates Bearer token from Authorization header.
// Stores claims in gin context under "claims" key.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "bearer token required"})
			return
		}
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := ParseToken(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		c.Set("claims", claims)
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// RequireRole enforces a minimum role level.
// Order: reader < writer < admin
func RequireRole(minRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claimsRaw, exists := c.Get("claims")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no claims in context"})
			return
		}
		claims := claimsRaw.(*Claims)
		if !hasRole(claims.Role, minRole) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":         "insufficient permissions",
				"required_role": minRole,
				"your_role":     claims.Role,
			})
			return
		}
		c.Next()
	}
}

// roleLevel maps role name to numeric level for comparison.
func roleLevel(role string) int {
	switch role {
	case "admin":
		return 3
	case "writer":
		return 2
	case "reader":
		return 1
	default:
		return 0
	}
}

func hasRole(userRole, required string) bool {
	return roleLevel(userRole) >= roleLevel(required)
}

// RequestID adds or propagates a request ID header.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = generateID()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func generateID() string {
	return strings.ReplaceAll(
		time.Now().Format("20060102150405.000000000"),
		".", "")
}

// RateLimit is a simple per-IP token bucket limiter.
// For production use golang.org/x/time/rate with per-user buckets.
func RateLimit(maxPerMin int) gin.HandlerFunc {
	type bucket struct {
		count    int
		windowAt time.Time
	}
	var (
		mu      = &sync.RWMutex{}
		buckets = make(map[string]*bucket)
	)
	_ = mu
	_ = buckets
	// Simple pass-through for now; Phase 2b replaces with proper rate limiter.
	return func(c *gin.Context) { c.Next() }
}
