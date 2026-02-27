package store

import (
	"errors"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Role constants used across services.
const (
	RoleAdmin  = "admin"
	RoleWriter = "writer"
	RoleReader = "reader"
)

// User represents an authenticated principal.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	Active       bool      `json:"active"`
}

// RefreshRecord tracks issued refresh tokens.
type RefreshRecord struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
}

// Store is a thread-safe, in-memory user + token store.
// In production this would be backed by Postgres / Redis.
type Store struct {
	mu           sync.RWMutex
	users        map[string]*User          // username → user
	usersByID    map[string]*User          // id → user
	refreshStore map[string]*RefreshRecord // token → record
}

// New returns a Store seeded with default users.
func New() *Store {
	s := &Store{
		users:        make(map[string]*User),
		usersByID:    make(map[string]*User),
		refreshStore: make(map[string]*RefreshRecord),
	}
	// Seed default users (change passwords via env in production).
	s.mustCreate("admin", envOr("ADMIN_PASSWORD", "admin123"), RoleAdmin)
	s.mustCreate("writer", envOr("WRITER_PASSWORD", "writer123"), RoleWriter)
	s.mustCreate("reader", envOr("READER_PASSWORD", "reader123"), RoleReader)
	return s
}

func (s *Store) mustCreate(username, password, role string) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	u := &User{
		ID:           username, // simple ID for dev; use UUID in production
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		CreatedAt:    time.Now(),
		Active:       true,
	}
	s.users[username] = u
	s.usersByID[username] = u
}

// Authenticate checks credentials and returns the user on success.
func (s *Store) Authenticate(username, password string) (*User, error) {
	s.mu.RLock()
	u, ok := s.users[username]
	s.mu.RUnlock()
	if !ok || !u.Active {
		return nil, errors.New("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid credentials")
	}
	return u, nil
}

// GetByID looks up a user by ID.
func (s *Store) GetByID(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.usersByID[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}

// StoreRefresh persists a refresh token.
func (s *Store) StoreRefresh(token, userID string, exp time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshStore[token] = &RefreshRecord{Token: token, UserID: userID, ExpiresAt: exp}
}

// ConsumeRefresh validates + invalidates a refresh token (rotation).
func (s *Store) ConsumeRefresh(token string) (*RefreshRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.refreshStore[token]
	if !ok {
		return nil, errors.New("refresh token not found")
	}
	if time.Now().After(rec.ExpiresAt) {
		delete(s.refreshStore, token)
		return nil, errors.New("refresh token expired")
	}
	delete(s.refreshStore, token) // single-use rotation
	return rec, nil
}

// CreateUser creates a new user (admin only operation).
func (s *Store) CreateUser(username, password, role string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[username]; exists {
		return nil, errors.New("username already exists")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &User{
		ID:           username,
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		CreatedAt:    time.Now(),
		Active:       true,
	}
	s.users[username] = u
	s.usersByID[username] = u
	return u, nil
}

// ListUsers returns all users (passwords omitted).
func (s *Store) ListUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
