//go:build integration

package store

import (
	"context"
	"os"
	"testing"

	"pennant/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresStore_ProjectCRUD(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := NewPostgresStore(context.Background(), dsn)
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.CreateProject(&model.Project{Key: "test-proj", Name: "Test"}))
	p, err := s.GetProject("test-proj")
	require.NoError(t, err)
	assert.Equal(t, "test-proj", p.Key)

	require.NoError(t, s.DeleteProject("test-proj"))
}
