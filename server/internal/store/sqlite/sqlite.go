package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"

	_ "modernc.org/sqlite"
)

const schemaVersion = 3

var migrations = []string{schemaV1, migrationV2, migrationV3}

const migrationV2 = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_agent_seq
	ON events(node_id, agent_seq) WHERE agent_seq IS NOT NULL;
`

const migrationV3 = `
ALTER TABLE jobs ADD COLUMN request TEXT NOT NULL DEFAULT '{}';
`

const schemaV1 = `
CREATE TABLE IF NOT EXISTS users (
	id            TEXT PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL,
	disabled      INTEGER NOT NULL DEFAULT 0,
	created_at    INTEGER NOT NULL,
	updated_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS access_tokens (
	id         TEXT PRIMARY KEY,
	token_hash TEXT NOT NULL UNIQUE,
	label      TEXT NOT NULL,
	created_by TEXT NOT NULL REFERENCES users(id),
	created_at INTEGER NOT NULL,
	expires_at INTEGER,
	revoked_at INTEGER
);

CREATE TABLE IF NOT EXISTS nodes (
	node_id          TEXT PRIMARY KEY,
	credential_hash  TEXT NOT NULL,
	hostname         TEXT NOT NULL,
	description      TEXT NOT NULL DEFAULT '',
	agent_version    TEXT NOT NULL DEFAULT '',
	protocol_version TEXT NOT NULL DEFAULT '',
	labels           TEXT NOT NULL DEFAULT '{}',
	gpus             TEXT NOT NULL DEFAULT '[]',
	status           TEXT NOT NULL,
	first_seen       INTEGER NOT NULL,
	last_seen        INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS pools (
	pool_id      TEXT PRIMARY KEY,
	name         TEXT NOT NULL UNIQUE,
	auto_created INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS node_pools (
	node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
	pool_id TEXT NOT NULL REFERENCES pools(pool_id) ON DELETE CASCADE,
	PRIMARY KEY (node_id, pool_id)
);

CREATE TABLE IF NOT EXISTS user_pools (
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	pool_id TEXT NOT NULL REFERENCES pools(pool_id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, pool_id)
);

CREATE TABLE IF NOT EXISTS dataset_pools (
	dataset_id TEXT NOT NULL,
	pool_id    TEXT NOT NULL REFERENCES pools(pool_id) ON DELETE CASCADE,
	PRIMARY KEY (dataset_id, pool_id)
);

CREATE TABLE IF NOT EXISTS user_pools (
	user_id TEXT NOT NULL REFERENCES users(id),
	pool_id TEXT NOT NULL REFERENCES pools(pool_id),
	PRIMARY KEY (user_id, pool_id)
);

CREATE TABLE IF NOT EXISTS dataset_pools (
	dataset_id TEXT NOT NULL,
	pool_id    TEXT NOT NULL REFERENCES pools(pool_id),
	PRIMARY KEY (dataset_id, pool_id)
);

CREATE TABLE IF NOT EXISTS datasets (
	dataset_id TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	version    TEXT NOT NULL DEFAULT '',
	kind       TEXT NOT NULL,
	backend    TEXT NOT NULL,
	uri        TEXT NOT NULL,
	sha256     TEXT NOT NULL,
	size_bytes INTEGER NOT NULL,
	created_by TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	job_id     TEXT NOT NULL DEFAULT '',
	deleted_at INTEGER
);

CREATE TABLE IF NOT EXISTS jobs (
	job_id         TEXT PRIMARY KEY,
	user_id        TEXT NOT NULL REFERENCES users(id),
	pool_id        TEXT NOT NULL REFERENCES pools(pool_id),
	node_id        TEXT NOT NULL DEFAULT '',
	engine         TEXT NOT NULL,
	engine_version TEXT NOT NULL DEFAULT '',
	config_toml    TEXT NOT NULL,
	assets         TEXT NOT NULL DEFAULT '[]',
	gpus           TEXT NOT NULL DEFAULT '[]',
	status         TEXT NOT NULL,
	exit_code      INTEGER,
	created_at     INTEGER NOT NULL,
	started_at     INTEGER,
	ended_at       INTEGER
);

CREATE TABLE IF NOT EXISTS events (
	seq       INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id   TEXT NOT NULL,
	job_id    TEXT NOT NULL DEFAULT '',
	type      TEXT NOT NULL,
	payload   TEXT NOT NULL DEFAULT '{}',
	server_ts INTEGER NOT NULL,
	agent_seq INTEGER,
	elapsed_s INTEGER,
	gap       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_events_job ON events(job_id, seq);
CREATE INDEX IF NOT EXISTS idx_events_node ON events(node_id, seq);

CREATE TABLE IF NOT EXISTS commands (
	command_id TEXT PRIMARY KEY,
	node_id    TEXT NOT NULL,
	type       TEXT NOT NULL,
	payload    TEXT NOT NULL DEFAULT '{}',
	issued_by  TEXT NOT NULL DEFAULT '',
	issued_at  INTEGER NOT NULL,
	acked_at   INTEGER
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

type DB struct {
	db *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := path
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared"
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &DB{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *DB) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	for i := v; i < schemaVersion && i < len(migrations); i++ {
		if _, err := s.db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migrate v%d: %w", i+1, err)
		}
	}
	_, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion))
	return err
}

func (s *DB) Close() error { return s.db.Close() }

func (s *DB) Users() store.Users               { return &usersRepo{q: s.db} }
func (s *DB) AccessTokens() store.AccessTokens { return &tokensRepo{q: s.db} }
func (s *DB) Nodes() store.Nodes               { return &nodesRepo{q: s.db} }
func (s *DB) Pools() store.Pools               { return &poolsRepo{q: s.db} }
func (s *DB) Datasets() store.Datasets         { return &datasetsRepo{q: s.db} }
func (s *DB) Jobs() store.Jobs                 { return &jobsRepo{q: s.db} }
func (s *DB) Settings() store.Settings         { return &settingsRepo{q: s.db} }

func (s *DB) Tx(ctx context.Context, fn func(st store.Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	ts := &DB{db: s.db}
	txs := &txStore{DB: ts, tx: tx}
	if err := fn(txs); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

type txStore struct {
	*DB
	tx *sql.Tx
}

func (t *txStore) Users() store.Users               { return &usersRepo{q: t.tx} }
func (t *txStore) AccessTokens() store.AccessTokens { return &tokensRepo{q: t.tx} }
func (t *txStore) Nodes() store.Nodes               { return &nodesRepo{q: t.tx} }
func (t *txStore) Pools() store.Pools               { return &poolsRepo{q: t.tx} }
func (t *txStore) Datasets() store.Datasets         { return &datasetsRepo{q: t.tx} }
func (t *txStore) Jobs() store.Jobs                 { return &jobsRepo{q: t.tx} }
func (t *txStore) Settings() store.Settings         { return &settingsRepo{q: t.tx} }
func (t *txStore) Tx(ctx context.Context, fn func(store.Store) error) error {
	return fn(t)
}
func (t *txStore) Close() error { return errors.New("store: cannot close a transaction") }

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func nullInt(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func conflictOr(err error) error {
	if isUniqueErr(err) {
		return store.ErrConflict
	}
	return err
}
