package sqlite

import (
	"context"
	"database/sql"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type poolsRepo struct{ q querier }

func (r *poolsRepo) Create(ctx context.Context, p *store.Pool) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO pools (pool_id, name, auto_created, created_at) VALUES (?,?,?,?)`,
		p.PoolID, p.Name, boolInt(p.AutoCreated), p.CreatedAt)
	return conflictOr(err)
}

func scanPool(row interface{ Scan(...any) error }) (*store.Pool, error) {
	var p store.Pool
	var auto int
	err := row.Scan(&p.PoolID, &p.Name, &auto, &p.CreatedAt)
	p.AutoCreated = auto != 0
	return &p, err
}

func (r *poolsRepo) Get(ctx context.Context, poolID string) (*store.Pool, error) {
	p, err := scanPool(r.q.QueryRowContext(ctx,
		`SELECT pool_id, name, auto_created, created_at FROM pools WHERE pool_id=?`, poolID))
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return p, err
}

func (r *poolsRepo) GetByName(ctx context.Context, name string) (*store.Pool, error) {
	p, err := scanPool(r.q.QueryRowContext(ctx,
		`SELECT pool_id, name, auto_created, created_at FROM pools WHERE name=?`, name))
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return p, err
}

func (r *poolsRepo) List(ctx context.Context) ([]*store.Pool, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT pool_id, name, auto_created, created_at FROM pools ORDER BY created_at, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.Pool
	for rows.Next() {
		p, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *poolsRepo) Delete(ctx context.Context, poolID string) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM pools WHERE pool_id=?`, poolID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func (r *poolsRepo) link(ctx context.Context, table, a, b string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT OR IGNORE INTO `+table+` VALUES (?,?)`, a, b)
	return err
}

func (r *poolsRepo) unlink(ctx context.Context, table, a, b string) error {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM `+table+` WHERE `+table[:len(table)-6]+`_id=? AND pool_id=?`, a, b)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func (r *poolsRepo) col(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *poolsRepo) AssignNode(ctx context.Context, nodeID, poolID string) error {
	return r.link(ctx, "node_pools", nodeID, poolID)
}

func (r *poolsRepo) UnassignNode(ctx context.Context, nodeID, poolID string) error {
	return r.unlink(ctx, "node_pools", nodeID, poolID)
}

func (r *poolsRepo) NodesOf(ctx context.Context, poolID string) ([]string, error) {
	return r.col(ctx, `SELECT node_id FROM node_pools WHERE pool_id=? ORDER BY node_id`, poolID)
}

func (r *poolsRepo) PoolsOfNode(ctx context.Context, nodeID string) ([]string, error) {
	return r.col(ctx, `SELECT pool_id FROM node_pools WHERE node_id=? ORDER BY pool_id`, nodeID)
}

func (r *poolsRepo) GrantUser(ctx context.Context, userID, poolID string) error {
	return r.link(ctx, "user_pools", userID, poolID)
}

func (r *poolsRepo) RevokeUser(ctx context.Context, userID, poolID string) error {
	return r.unlink(ctx, "user_pools", userID, poolID)
}

func (r *poolsRepo) PoolsOfUser(ctx context.Context, userID string) ([]string, error) {
	return r.col(ctx, `SELECT pool_id FROM user_pools WHERE user_id=? ORDER BY pool_id`, userID)
}

func (r *poolsRepo) UsersOf(ctx context.Context, poolID string) ([]string, error) {
	return r.col(ctx, `SELECT user_id FROM user_pools WHERE pool_id=? ORDER BY user_id`, poolID)
}

func (r *poolsRepo) GrantDataset(ctx context.Context, datasetID, poolID string) error {
	return r.link(ctx, "dataset_pools", datasetID, poolID)
}

func (r *poolsRepo) RevokeDataset(ctx context.Context, datasetID, poolID string) error {
	return r.unlink(ctx, "dataset_pools", datasetID, poolID)
}

func (r *poolsRepo) PoolsOfDataset(ctx context.Context, datasetID string) ([]string, error) {
	return r.col(ctx, `SELECT pool_id FROM dataset_pools WHERE dataset_id=? ORDER BY pool_id`, datasetID)
}
