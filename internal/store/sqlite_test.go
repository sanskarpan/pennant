package store

import (
	"os"
	"path/filepath"
	"testing"

	"pennant/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSqlite(t *testing.T) *SqliteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := NewSqliteStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSqliteStore_ProjectCRUD(t *testing.T) {
	s := newTestSqlite(t)

	require.NoError(t, s.CreateProject(&model.Project{Key: "proj1", Name: "Project 1"}))

	p, err := s.GetProject("proj1")
	require.NoError(t, err)
	assert.Equal(t, "proj1", p.Key)

	list, err := s.ListProjects()
	require.NoError(t, err)
	assert.Len(t, list, 1)

	require.NoError(t, s.UpdateProject(&model.Project{Key: "proj1", Name: "Updated"}))
	p, _ = s.GetProject("proj1")
	assert.Equal(t, "Updated", p.Name)

	require.NoError(t, s.DeleteProject("proj1"))
	_, err = s.GetProject("proj1")
	assert.Error(t, err)
}

func TestSqliteStore_FlagCRUD(t *testing.T) {
	s := newTestSqlite(t)
	require.NoError(t, s.CreateProject(&model.Project{Key: "proj"}))

	flag := &model.Flag{
		Key:  "my-flag",
		Name: "My Flag",
		Type: model.TypeBoolean,
		Variations: []model.Variation{
			{ID: "v0", Value: []byte("false")},
			{ID: "v1", Value: []byte("true")},
		},
	}
	require.NoError(t, s.CreateFlag("proj", flag))

	f, err := s.GetFlag("proj", "my-flag")
	require.NoError(t, err)
	assert.Equal(t, "my-flag", f.Key)
	assert.Len(t, f.Variations, 2)

	require.NoError(t, s.DeleteFlag("proj", "my-flag"))
	_, err = s.GetFlag("proj", "my-flag")
	assert.Error(t, err)
}

func TestSqliteStore_FlagConfig(t *testing.T) {
	s := newTestSqlite(t)
	require.NoError(t, s.CreateProject(&model.Project{Key: "proj"}))

	offVar := 0
	trueVar := 1
	cfg := &model.FlagConfig{
		On:           true,
		OffVariation: &offVar,
		Fallthrough:  model.VariationOrRollout{Variation: &trueVar},
		Salt:         "my-flag",
	}

	require.NoError(t, s.UpsertFlagConfig("proj", "prod", "my-flag", cfg))

	got, err := s.GetFlagConfig("proj", "prod", "my-flag")
	require.NoError(t, err)
	assert.True(t, got.On)

	// Upsert again (update)
	cfg.On = false
	require.NoError(t, s.UpsertFlagConfig("proj", "prod", "my-flag", cfg))
	got, _ = s.GetFlagConfig("proj", "prod", "my-flag")
	assert.False(t, got.On)
}

func TestSqliteStore_Versioning(t *testing.T) {
	s := newTestSqlite(t)

	v1, err := s.IncrementEnvVersion("proj", "prod")
	require.NoError(t, err)
	assert.Equal(t, int64(1), v1)

	v2, err := s.IncrementEnvVersion("proj", "prod")
	require.NoError(t, err)
	assert.Equal(t, int64(2), v2)
}

func TestSqliteStore_Audit(t *testing.T) {
	s := newTestSqlite(t)

	require.NoError(t, s.AppendAudit(&AuditEntry{
		Actor:      "user@example.com",
		Action:     "update",
		Resource:   "flag",
		ResourceID: "my-flag",
		At:         1000,
	}))

	entries, err := s.ListAudit("proj", 10)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "user@example.com", entries[0].Actor)
}

func TestSqliteStore_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	// Write data
	s1, err := NewSqliteStore(path)
	require.NoError(t, err)
	require.NoError(t, s1.CreateProject(&model.Project{Key: "persist-proj", Name: "Persistent"}))
	s1.Close()

	// Reopen and verify
	s2, err := NewSqliteStore(path)
	require.NoError(t, err)
	defer s2.Close()
	p, err := s2.GetProject("persist-proj")
	require.NoError(t, err)
	assert.Equal(t, "Persistent", p.Name, "data must survive store close+reopen")
}

// Ensure unused import doesn't break build
var _ = os.DevNull
