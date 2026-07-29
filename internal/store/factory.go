package store

import (
	"context"
	"fmt"
	"os"
)

// NewStore creates either a PostgresStore (if DATABASE_URL is set) or
// a SqliteStore (if SQLITE_PATH is set) or an in-memory store as fallback.
func NewStore() (ConfigStore, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		s, err := NewPostgresStore(context.Background(), dsn)
		if err != nil {
			return nil, fmt.Errorf("postgres: %w", err)
		}
		return s, nil
	}
	if path := os.Getenv("SQLITE_PATH"); path != "" {
		s, err := NewSqliteStore(path)
		if err != nil {
			return nil, fmt.Errorf("sqlite: %w", err)
		}
		return s, nil
	}
	return NewMemoryStore(), nil
}
