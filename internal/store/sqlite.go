package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"pennant/internal/model"
)

// SqliteStore implements ConfigStore using SQLite.
// Complex structs (Flag.Variations, FlagConfig, Segment.Rules, etc.) are
// stored as JSON blobs — avoids fragile schema migrations for every model change.
type SqliteStore struct {
	db *sql.DB
}

// NewSqliteStore opens (or creates) a SQLite database at the given path and
// runs the schema migrations.
func NewSqliteStore(path string) (*SqliteStore, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite allows only one writer at a time
	s := &SqliteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *SqliteStore) Close() error { return s.db.Close() }

func (s *SqliteStore) migrate() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS projects (
		key TEXT PRIMARY KEY,
		data TEXT NOT NULL -- JSON model.Project
	);

	CREATE TABLE IF NOT EXISTS environments (
		project_key TEXT NOT NULL,
		key TEXT NOT NULL,
		data TEXT NOT NULL, -- JSON model.Environment
		PRIMARY KEY (project_key, key)
	);

	CREATE TABLE IF NOT EXISTS flags (
		project_key TEXT NOT NULL,
		key TEXT NOT NULL,
		data TEXT NOT NULL, -- JSON model.Flag
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (project_key, key)
	);

	CREATE TABLE IF NOT EXISTS flag_configs (
		project_key TEXT NOT NULL,
		env_key TEXT NOT NULL,
		flag_key TEXT NOT NULL,
		data TEXT NOT NULL, -- JSON model.FlagConfig
		PRIMARY KEY (project_key, env_key, flag_key)
	);

	CREATE TABLE IF NOT EXISTS segments (
		project_key TEXT NOT NULL,
		key TEXT NOT NULL,
		data TEXT NOT NULL, -- JSON model.Segment
		PRIMARY KEY (project_key, key)
	);

	CREATE TABLE IF NOT EXISTS env_versions (
		project_key TEXT NOT NULL,
		env_key TEXT NOT NULL,
		version INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (project_key, env_key)
	);

	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		actor TEXT NOT NULL,
		action TEXT NOT NULL,
		resource TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		before_json TEXT,
		after_json TEXT,
		at INTEGER NOT NULL
	);
	`)
	return err
}

// ---- Projects ----

func (s *SqliteStore) GetProject(key string) (*model.Project, error) {
	row := s.db.QueryRow(`SELECT data FROM projects WHERE key = ?`, key)
	var data string
	if err := row.Scan(&data); err == sql.ErrNoRows {
		return nil, fmt.Errorf("project %q not found", key)
	} else if err != nil {
		return nil, err
	}
	var p model.Project
	return &p, json.Unmarshal([]byte(data), &p)
}

func (s *SqliteStore) ListProjects() ([]*model.Project, error) {
	rows, err := s.db.Query(`SELECT data FROM projects ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Project
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var p model.Project
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *SqliteStore) CreateProject(p *model.Project) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO projects (key, data) VALUES (?, ?)`, p.Key, string(data))
	if err != nil {
		return fmt.Errorf("project %q already exists", p.Key)
	}
	return nil
}

func (s *SqliteStore) UpdateProject(p *model.Project) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE projects SET data = ? WHERE key = ?`, string(data), p.Key)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project %q not found", p.Key)
	}
	return nil
}

func (s *SqliteStore) DeleteProject(key string) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE key = ?`, key)
	return err
}

// ---- Environments ----

func (s *SqliteStore) GetEnvironment(projectKey, envKey string) (*model.Environment, error) {
	row := s.db.QueryRow(`SELECT data FROM environments WHERE project_key = ? AND key = ?`, projectKey, envKey)
	var data string
	if err := row.Scan(&data); err == sql.ErrNoRows {
		return nil, fmt.Errorf("environment %q not found", envKey)
	} else if err != nil {
		return nil, err
	}
	var env model.Environment
	return &env, json.Unmarshal([]byte(data), &env)
}

func (s *SqliteStore) ListEnvironments(projectKey string) ([]*model.Environment, error) {
	rows, err := s.db.Query(`SELECT data FROM environments WHERE project_key = ? ORDER BY key`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Environment
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var env model.Environment
		if err := json.Unmarshal([]byte(data), &env); err != nil {
			return nil, err
		}
		out = append(out, &env)
	}
	return out, rows.Err()
}

func (s *SqliteStore) CreateEnvironment(projectKey string, env *model.Environment) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(`INSERT INTO environments (project_key, key, data) VALUES (?, ?, ?)`,
		projectKey, env.Key, string(data))
	if err != nil {
		return fmt.Errorf("environment %q already exists", env.Key)
	}
	_, err = tx.Exec(`INSERT OR IGNORE INTO env_versions (project_key, env_key, version) VALUES (?, ?, 0)`,
		projectKey, env.Key)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SqliteStore) UpdateEnvironment(projectKey string, env *model.Environment) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE environments SET data = ? WHERE project_key = ? AND key = ?`,
		string(data), projectKey, env.Key)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("environment %q not found", env.Key)
	}
	return nil
}

func (s *SqliteStore) DeleteEnvironment(projectKey, envKey string) error {
	_, err := s.db.Exec(`DELETE FROM environments WHERE project_key = ? AND key = ?`, projectKey, envKey)
	return err
}

// ---- Flags ----

func (s *SqliteStore) GetFlag(projectKey, flagKey string) (*model.Flag, error) {
	row := s.db.QueryRow(`SELECT data FROM flags WHERE project_key = ? AND key = ?`, projectKey, flagKey)
	var data string
	if err := row.Scan(&data); err == sql.ErrNoRows {
		return nil, fmt.Errorf("flag %q not found", flagKey)
	} else if err != nil {
		return nil, err
	}
	var f model.Flag
	return &f, json.Unmarshal([]byte(data), &f)
}

func (s *SqliteStore) ListFlags(projectKey string) ([]*model.Flag, error) {
	rows, err := s.db.Query(`SELECT data FROM flags WHERE project_key = ? ORDER BY key`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Flag
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var f model.Flag
		if err := json.Unmarshal([]byte(data), &f); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (s *SqliteStore) CreateFlag(projectKey string, flag *model.Flag) error {
	flag.CreatedAt = time.Now()
	flag.UpdatedAt = time.Now()
	data, err := json.Marshal(flag)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO flags (project_key, key, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		projectKey, flag.Key, string(data),
		flag.CreatedAt.UnixMilli(), flag.UpdatedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("flag %q already exists", flag.Key)
	}
	return nil
}

func (s *SqliteStore) UpdateFlag(projectKey string, flag *model.Flag) error {
	flag.UpdatedAt = time.Now()
	data, err := json.Marshal(flag)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE flags SET data = ?, updated_at = ? WHERE project_key = ? AND key = ?`,
		string(data), flag.UpdatedAt.UnixMilli(), projectKey, flag.Key)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("flag %q not found", flag.Key)
	}
	return nil
}

func (s *SqliteStore) DeleteFlag(projectKey, flagKey string) error {
	_, err := s.db.Exec(`DELETE FROM flags WHERE project_key = ? AND key = ?`, projectKey, flagKey)
	return err
}

// ---- FlagConfigs ----

func (s *SqliteStore) GetFlagConfig(projectKey, envKey, flagKey string) (*model.FlagConfig, error) {
	row := s.db.QueryRow(`SELECT data FROM flag_configs WHERE project_key = ? AND env_key = ? AND flag_key = ?`,
		projectKey, envKey, flagKey)
	var data string
	if err := row.Scan(&data); err == sql.ErrNoRows {
		return nil, fmt.Errorf("flag config not found for %q/%q/%q", projectKey, envKey, flagKey)
	} else if err != nil {
		return nil, err
	}
	var cfg model.FlagConfig
	return &cfg, json.Unmarshal([]byte(data), &cfg)
}

func (s *SqliteStore) UpsertFlagConfig(projectKey, envKey, flagKey string, cfg *model.FlagConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO flag_configs (project_key, env_key, flag_key, data) VALUES (?, ?, ?, ?)
		ON CONFLICT(project_key, env_key, flag_key) DO UPDATE SET data = excluded.data`,
		projectKey, envKey, flagKey, string(data))
	return err
}

// ---- Segments ----

func (s *SqliteStore) GetSegment(projectKey, segKey string) (*model.Segment, error) {
	row := s.db.QueryRow(`SELECT data FROM segments WHERE project_key = ? AND key = ?`, projectKey, segKey)
	var data string
	if err := row.Scan(&data); err == sql.ErrNoRows {
		return nil, fmt.Errorf("segment %q not found", segKey)
	} else if err != nil {
		return nil, err
	}
	var seg model.Segment
	return &seg, json.Unmarshal([]byte(data), &seg)
}

func (s *SqliteStore) ListSegments(projectKey string) ([]*model.Segment, error) {
	rows, err := s.db.Query(`SELECT data FROM segments WHERE project_key = ? ORDER BY key`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Segment
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var seg model.Segment
		if err := json.Unmarshal([]byte(data), &seg); err != nil {
			return nil, err
		}
		out = append(out, &seg)
	}
	return out, rows.Err()
}

func (s *SqliteStore) CreateSegment(projectKey string, seg *model.Segment) error {
	data, err := json.Marshal(seg)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO segments (project_key, key, data) VALUES (?, ?, ?)`,
		projectKey, seg.Key, string(data))
	if err != nil {
		return fmt.Errorf("segment %q already exists", seg.Key)
	}
	return nil
}

func (s *SqliteStore) UpdateSegment(projectKey string, seg *model.Segment) error {
	data, err := json.Marshal(seg)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE segments SET data = ? WHERE project_key = ? AND key = ?`,
		string(data), projectKey, seg.Key)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("segment %q not found", seg.Key)
	}
	return nil
}

func (s *SqliteStore) DeleteSegment(projectKey, segKey string) error {
	_, err := s.db.Exec(`DELETE FROM segments WHERE project_key = ? AND key = ?`, projectKey, segKey)
	return err
}

// ---- Versioning ----

func (s *SqliteStore) GetEnvVersion(projectKey, envKey string) (int64, error) {
	row := s.db.QueryRow(`SELECT version FROM env_versions WHERE project_key = ? AND env_key = ?`,
		projectKey, envKey)
	var v int64
	if err := row.Scan(&v); err == sql.ErrNoRows {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	return v, nil
}

func (s *SqliteStore) IncrementEnvVersion(projectKey, envKey string) (int64, error) {
	_, err := s.db.Exec(`
		INSERT INTO env_versions (project_key, env_key, version) VALUES (?, ?, 1)
		ON CONFLICT(project_key, env_key) DO UPDATE SET version = version + 1`,
		projectKey, envKey)
	if err != nil {
		return 0, err
	}
	return s.GetEnvVersion(projectKey, envKey)
}

// ---- Audit ----

func (s *SqliteStore) AppendAudit(entry *AuditEntry) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_log (actor, action, resource, resource_id, before_json, after_json, at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.Actor, entry.Action, entry.Resource, entry.ResourceID,
		string(entry.Before), string(entry.After), entry.At)
	return err
}

func (s *SqliteStore) ListAudit(projectKey string, limit int) ([]*AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT actor, action, resource, resource_id, before_json, after_json, at
		FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditEntry
	for rows.Next() {
		var e AuditEntry
		var beforeStr, afterStr string
		if err := rows.Scan(&e.Actor, &e.Action, &e.Resource, &e.ResourceID, &beforeStr, &afterStr, &e.At); err != nil {
			return nil, err
		}
		e.Before = []byte(beforeStr)
		e.After = []byte(afterStr)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// compile-time interface check
var _ ConfigStore = (*SqliteStore)(nil)
