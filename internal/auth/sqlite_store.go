package auth

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SqliteUserStore persists users to SQLite; refresh tokens remain in-memory
// (they're short-lived and ephemeral by design).
type SqliteUserStore struct {
	db            *sql.DB
	mu            sync.RWMutex
	refreshTokens map[string]refreshEntry
}

// NewSqliteUserStore opens (or creates) a SQLite database at path and runs migrations.
func NewSqliteUserStore(path string) (*SqliteUserStore, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	s := &SqliteUserStore{
		db:            db,
		refreshTokens: make(map[string]refreshEntry),
	}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SqliteUserStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			role TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)
	`)
	return err
}

func (s *SqliteUserStore) CreateUser(u *User) error {
	if u.ID == "" {
		u.ID = randomHex(16)
	}
	u.CreatedAt = time.Now()
	_, err := s.db.Exec(
		`INSERT INTO users (id, email, name, role, password_hash, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Name, string(u.Role), u.PasswordHash, u.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("user %q already exists", u.Email)
	}
	return nil
}

func (s *SqliteUserStore) GetByEmail(email string) (*User, bool) {
	u := &User{}
	var roleStr string
	var createdAt int64
	err := s.db.QueryRow(
		`SELECT id, email, name, role, password_hash, created_at FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &roleStr, &u.PasswordHash, &createdAt)
	if err != nil {
		return nil, false
	}
	u.Role = Role(roleStr)
	u.CreatedAt = time.UnixMilli(createdAt)
	return u, true
}

func (s *SqliteUserStore) GetByID(id string) (*User, bool) {
	u := &User{}
	var roleStr string
	var createdAt int64
	err := s.db.QueryRow(
		`SELECT id, email, name, role, password_hash, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &roleStr, &u.PasswordHash, &createdAt)
	if err != nil {
		return nil, false
	}
	u.Role = Role(roleStr)
	u.CreatedAt = time.UnixMilli(createdAt)
	return u, true
}

func (s *SqliteUserStore) IssueRefreshToken(userID string) string {
	token := randomHex(32)
	s.mu.Lock()
	s.refreshTokens[token] = refreshEntry{
		UserID:    userID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	s.mu.Unlock()
	return token
}

func (s *SqliteUserStore) ValidateRefreshToken(token string) (string, bool) {
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

func (s *SqliteUserStore) Close() error { return s.db.Close() }
