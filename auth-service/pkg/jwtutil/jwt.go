package jwtutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload embedded in every token.
type Claims struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	TokenType string `json:"token_type"` // "access" or "refresh"
	jwt.RegisteredClaims
}

// TokenPair holds both access and refresh tokens returned at login.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds until access token expiry
}

const (
	AccessTokenDuration  = 15 * time.Minute
	RefreshTokenDuration = 7 * 24 * time.Hour
)

func jwtSecret() []byte {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		s = "telemetry-search-dev-secret-change-in-production"
	}
	return []byte(s)
}

// GeneratePair creates an access + refresh token pair for the given user.
// Each token has a unique jti (JWT ID) to prevent token reuse collisions.
func GeneratePair(userID, username, role string) (TokenPair, error) {
	now := time.Now()

	access, err := generateToken(userID, username, role, "access", now.Add(AccessTokenDuration))
	if err != nil {
		return TokenPair{}, err
	}

	refresh, err := generateToken(userID, username, role, "refresh", now.Add(RefreshTokenDuration))
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(AccessTokenDuration.Seconds()),
	}, nil
}

func generateToken(userID, username, role, tokenType string, expiry time.Time) (string, error) {
	jti, err := randomHex(16)
	if err != nil {
		return "", err
	}
	claims := Claims{
		UserID:    userID,
		Username:  username,
		Role:      role,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti, // unique per token — prevents map key collisions in token store
			ExpiresAt: jwt.NewNumericDate(expiry),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "telemetry-search",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret())
}

// Parse validates a token string and returns the claims.
// Returns an error if the token is expired, malformed, or has invalid signature.
func Parse(tokenStr string) (*Claims, error) {
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

// ParseRefresh is like Parse but also enforces token_type == "refresh".
// This prevents an access token being used as a refresh token.
func ParseRefresh(tokenStr string) (*Claims, error) {
	claims, err := Parse(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != "refresh" {
		return nil, errors.New("not a refresh token")
	}
	return claims, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
