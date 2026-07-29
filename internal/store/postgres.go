package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"pennant/internal/model"
)

// PostgresStore implements ConfigStore using PostgreSQL.
// All complex fields are stored as JSONB for schema flexibility.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects to PostgreSQL and runs migrations.
// dsn format: postgres://user:pass@host:5432/dbname?sslmode=disable
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgx connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgx ping: %w", err)
	}
	s := &PostgresStore{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

func (s *PostgresStore) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
	CREATE TABLE IF NOT EXISTS projects (
		key TEXT PRIMARY KEY,
		data JSONB NOT NULL
	);

	CREATE TABLE IF NOT EXISTS environments (
		project_key TEXT NOT NULL,
		key TEXT NOT NULL,
		data JSONB NOT NULL,
		PRIMARY KEY (project_key, key)
	);

	CREATE TABLE IF NOT EXISTS flags (
		project_key TEXT NOT NULL,
		key TEXT NOT NULL,
		data JSONB NOT NULL,
		created_at BIGINT NOT NULL DEFAULT 0,
		updated_at BIGINT NOT NULL DEFAULT 0,
		PRIMARY KEY (project_key, key)
	);

	CREATE TABLE IF NOT EXISTS flag_configs (
		project_key TEXT NOT NULL,
		env_key TEXT NOT NULL,
		flag_key TEXT NOT NULL,
		data JSONB NOT NULL,
		PRIMARY KEY (project_key, env_key, flag_key)
	);

	CREATE TABLE IF NOT EXISTS segments (
		project_key TEXT NOT NULL,
		key TEXT NOT NULL,
		data JSONB NOT NULL,
		PRIMARY KEY (project_key, key)
	);

	CREATE TABLE IF NOT EXISTS env_versions (
		project_key TEXT NOT NULL,
		env_key TEXT NOT NULL,
		version BIGINT NOT NULL DEFAULT 0,
		PRIMARY KEY (project_key, env_key)
	);

	CREATE TABLE IF NOT EXISTS audit_log (
		id BIGSERIAL PRIMARY KEY,
		actor TEXT NOT NULL,
		action TEXT NOT NULL,
		resource TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		before_json TEXT,
		after_json TEXT,
		at BIGINT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_audit_log_at ON audit_log (at DESC);
	`)
	return err
}

// ---- helper ----
func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func unmarshal[T any](data string, out *T) error {
	return json.Unmarshal([]byte(data), out)
}

// ---- Projects ----

func (s *PostgresStore) GetProject(key string) (*model.Project, error) {
	ctx := context.Background()
	var data string
	err := s.pool.QueryRow(ctx, `SELECT data FROM projects WHERE key = $1`, key).Scan(&data)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("project %q not found", key)
	} else if err != nil {
		return nil, err
	}
	var p model.Project
	return &p, unmarshal(data, &p)
}

func (s *PostgresStore) ListProjects() ([]*model.Project, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT data FROM projects ORDER BY key`)
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
		if err := unmarshal(data, &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateProject(p *model.Project) error {
	ctx := context.Background()
	data, err := marshal(p)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO projects (key, data) VALUES ($1, $2)`, p.Key, data)
	if err != nil {
		return fmt.Errorf("project %q already exists", p.Key)
	}
	return nil
}

func (s *PostgresStore) UpdateProject(p *model.Project) error {
	ctx := context.Background()
	data, err := marshal(p)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE projects SET data = $1 WHERE key = $2`, data, p.Key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("project %q not found", p.Key)
	}
	return nil
}

func (s *PostgresStore) DeleteProject(key string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE key = $1`, key)
	return err
}

// ---- Environments ----

func (s *PostgresStore) GetEnvironment(projectKey, envKey string) (*model.Environment, error) {
	ctx := context.Background()
	var data string
	err := s.pool.QueryRow(ctx, `SELECT data FROM environments WHERE project_key = $1 AND key = $2`, projectKey, envKey).Scan(&data)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("environment %q not found", envKey)
	} else if err != nil {
		return nil, err
	}
	var env model.Environment
	return &env, unmarshal(data, &env)
}

func (s *PostgresStore) ListEnvironments(projectKey string) ([]*model.Environment, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT data FROM environments WHERE project_key = $1 ORDER BY key`, projectKey)
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
		if err := unmarshal(data, &env); err != nil {
			return nil, err
		}
		out = append(out, &env)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateEnvironment(projectKey string, env *model.Environment) error {
	ctx := context.Background()
	data, err := marshal(env)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO environments (project_key, key, data) VALUES ($1, $2, $3)`, projectKey, env.Key, data)
	if err != nil {
		return fmt.Errorf("environment %q already exists", env.Key)
	}
	_, err = tx.Exec(ctx, `INSERT INTO env_versions (project_key, env_key, version) VALUES ($1, $2, 0) ON CONFLICT DO NOTHING`, projectKey, env.Key)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) UpdateEnvironment(projectKey string, env *model.Environment) error {
	ctx := context.Background()
	data, err := marshal(env)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE environments SET data = $1 WHERE project_key = $2 AND key = $3`, data, projectKey, env.Key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("environment %q not found", env.Key)
	}
	return nil
}

func (s *PostgresStore) DeleteEnvironment(projectKey, envKey string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `DELETE FROM environments WHERE project_key = $1 AND key = $2`, projectKey, envKey)
	return err
}

// ---- Flags ----

func (s *PostgresStore) GetFlag(projectKey, flagKey string) (*model.Flag, error) {
	ctx := context.Background()
	var data string
	err := s.pool.QueryRow(ctx, `SELECT data FROM flags WHERE project_key = $1 AND key = $2`, projectKey, flagKey).Scan(&data)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("flag %q not found", flagKey)
	} else if err != nil {
		return nil, err
	}
	var f model.Flag
	return &f, unmarshal(data, &f)
}

func (s *PostgresStore) ListFlags(projectKey string) ([]*model.Flag, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT data FROM flags WHERE project_key = $1 ORDER BY key`, projectKey)
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
		if err := unmarshal(data, &f); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateFlag(projectKey string, flag *model.Flag) error {
	ctx := context.Background()
	flag.CreatedAt = time.Now()
	flag.UpdatedAt = time.Now()
	data, err := marshal(flag)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO flags (project_key, key, data, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		projectKey, flag.Key, data, flag.CreatedAt.UnixMilli(), flag.UpdatedAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("flag %q already exists", flag.Key)
	}
	return nil
}

func (s *PostgresStore) UpdateFlag(projectKey string, flag *model.Flag) error {
	ctx := context.Background()
	flag.UpdatedAt = time.Now()
	data, err := marshal(flag)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE flags SET data = $1, updated_at = $2 WHERE project_key = $3 AND key = $4`,
		data, flag.UpdatedAt.UnixMilli(), projectKey, flag.Key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("flag %q not found", flag.Key)
	}
	return nil
}

func (s *PostgresStore) DeleteFlag(projectKey, flagKey string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `DELETE FROM flags WHERE project_key = $1 AND key = $2`, projectKey, flagKey)
	return err
}

// ---- FlagConfigs ----

func (s *PostgresStore) GetFlagConfig(projectKey, envKey, flagKey string) (*model.FlagConfig, error) {
	ctx := context.Background()
	var data string
	err := s.pool.QueryRow(ctx, `SELECT data FROM flag_configs WHERE project_key = $1 AND env_key = $2 AND flag_key = $3`, projectKey, envKey, flagKey).Scan(&data)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("flag config not found")
	} else if err != nil {
		return nil, err
	}
	var cfg model.FlagConfig
	return &cfg, unmarshal(data, &cfg)
}

func (s *PostgresStore) UpsertFlagConfig(projectKey, envKey, flagKey string, cfg *model.FlagConfig) error {
	ctx := context.Background()
	data, err := marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO flag_configs (project_key, env_key, flag_key, data) VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_key, env_key, flag_key) DO UPDATE SET data = EXCLUDED.data`,
		projectKey, envKey, flagKey, data)
	return err
}

// ---- Segments ----

func (s *PostgresStore) GetSegment(projectKey, segKey string) (*model.Segment, error) {
	ctx := context.Background()
	var data string
	err := s.pool.QueryRow(ctx, `SELECT data FROM segments WHERE project_key = $1 AND key = $2`, projectKey, segKey).Scan(&data)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("segment %q not found", segKey)
	} else if err != nil {
		return nil, err
	}
	var seg model.Segment
	return &seg, unmarshal(data, &seg)
}

func (s *PostgresStore) ListSegments(projectKey string) ([]*model.Segment, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT data FROM segments WHERE project_key = $1 ORDER BY key`, projectKey)
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
		if err := unmarshal(data, &seg); err != nil {
			return nil, err
		}
		out = append(out, &seg)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateSegment(projectKey string, seg *model.Segment) error {
	ctx := context.Background()
	data, err := marshal(seg)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO segments (project_key, key, data) VALUES ($1, $2, $3)`, projectKey, seg.Key, data)
	if err != nil {
		return fmt.Errorf("segment %q already exists", seg.Key)
	}
	return nil
}

func (s *PostgresStore) UpdateSegment(projectKey string, seg *model.Segment) error {
	ctx := context.Background()
	data, err := marshal(seg)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE segments SET data = $1 WHERE project_key = $2 AND key = $3`, data, projectKey, seg.Key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("segment %q not found", seg.Key)
	}
	return nil
}

func (s *PostgresStore) DeleteSegment(projectKey, segKey string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `DELETE FROM segments WHERE project_key = $1 AND key = $2`, projectKey, segKey)
	return err
}

// ---- Versioning ----

func (s *PostgresStore) GetEnvVersion(projectKey, envKey string) (int64, error) {
	ctx := context.Background()
	var v int64
	err := s.pool.QueryRow(ctx, `SELECT version FROM env_versions WHERE project_key = $1 AND env_key = $2`, projectKey, envKey).Scan(&v)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func (s *PostgresStore) IncrementEnvVersion(projectKey, envKey string) (int64, error) {
	ctx := context.Background()
	var v int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO env_versions (project_key, env_key, version) VALUES ($1, $2, 1)
		ON CONFLICT (project_key, env_key) DO UPDATE SET version = env_versions.version + 1
		RETURNING version`, projectKey, envKey).Scan(&v)
	return v, err
}

// ---- Audit ----

func (s *PostgresStore) AppendAudit(entry *AuditEntry) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (actor, action, resource, resource_id, before_json, after_json, at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.Actor, entry.Action, entry.Resource, entry.ResourceID,
		string(entry.Before), string(entry.After), entry.At)
	return err
}

func (s *PostgresStore) ListAudit(projectKey string, limit int) ([]*AuditEntry, error) {
	ctx := context.Background()
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT actor, action, resource, resource_id, COALESCE(before_json,''), COALESCE(after_json,''), at
		FROM audit_log ORDER BY id DESC LIMIT $1`, limit)
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

// Compile-time interface check
var _ ConfigStore = (*PostgresStore)(nil)
