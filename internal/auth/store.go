package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// UserStorer is the interface satisfied by UserStore and SqliteUserStore.
type UserStorer interface {
	CreateUser(u *User) error
	GetByEmail(email string) (*User, bool)
	GetByID(id string) (*User, bool)
	IssueRefreshToken(userID string) string
	ValidateRefreshToken(token string) (string, bool)
}

// UserStore manages users and refresh tokens (in-memory for now).
type UserStore struct {
	mu            sync.RWMutex
	users         map[string]*User        // id -> user
	byEmail       map[string]*User        // email -> user
	refreshTokens map[string]refreshEntry // token -> entry
}

type refreshEntry struct {
	UserID    string
	ExpiresAt time.Time
}

func NewUserStore() *UserStore {
	return &UserStore{
		users:         make(map[string]*User),
		byEmail:       make(map[string]*User),
		refreshTokens: make(map[string]refreshEntry),
	}
}

func (s *UserStore) CreateUser(u *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[u.Email]; exists {
		return fmt.Errorf("user %q already exists", u.Email)
	}
	if u.ID == "" {
		u.ID = randomHex(16)
	}
	u.CreatedAt = time.Now()
	s.users[u.ID] = u
	s.byEmail[u.Email] = u
	return nil
}

func (s *UserStore) GetByEmail(email string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byEmail[email]
	return u, ok
}

func (s *UserStore) GetByID(id string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok
}

func (s *UserStore) IssueRefreshToken(userID string) string {
	token := randomHex(32)
	s.mu.Lock()
	s.refreshTokens[token] = refreshEntry{
		UserID:    userID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	s.mu.Unlock()
	return token
}

func (s *UserStore) ValidateRefreshToken(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.refreshTokens[token]
	if !ok || time.Now().After(entry.ExpiresAt) {
		delete(s.refreshTokens, token)
		return "", false
	}
	// Rotate: delete old token, caller will issue a new one
	delete(s.refreshTokens, token)
	return entry.UserID, true
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
