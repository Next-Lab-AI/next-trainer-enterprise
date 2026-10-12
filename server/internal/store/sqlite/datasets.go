package sqlite

import (
	"context"
	"database/sql"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type datasetsRepo struct{ q querier }

const datasetCols = "dataset_id, name, version, kind, backend, uri, sha256, size_bytes, created_by, created_at, job_id, deleted_at"

func scanDataset(row interface{ Scan(...any) error }) (*store.Dataset, error) {
	var d store.Dataset
	var del sql.NullInt64
	err := row.Scan(&d.DatasetID, &d.Name, &d.Version, &d.Kind, &d.Backend, &d.URI,
		&d.SHA256, &d.SizeBytes, &d.CreatedBy, &d.CreatedAt, &d.JobID, &del)
	d.DeletedAt = intPtr(del)
	return &d, err
}

func (r *datasetsRepo) Create(ctx context.Context, d *store.Dataset) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO datasets (`+datasetCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.DatasetID, d.Name, d.Version, d.Kind, d.Backend, d.URI,
		d.SHA256, d.SizeBytes, d.CreatedBy, d.CreatedAt, d.JobID, nullInt(d.DeletedAt))
	return conflictOr(err)
}

func (r *datasetsRepo) Get(ctx context.Context, datasetID string) (*store.Dataset, error) {
	d, err := scanDataset(r.q.QueryRowContext(ctx,
		`SELECT `+datasetCols+` FROM datasets WHERE dataset_id=?`, datasetID))
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return d, err
}

func (r *datasetsRepo) List(ctx context.Context, includeDeleted bool) ([]*store.Dataset, error) {
	q := `SELECT ` + datasetCols + ` FROM datasets`
	if !includeDeleted {
		q += ` WHERE deleted_at IS NULL`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := r.q.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.Dataset
	for rows.Next() {
		d, err := scanDataset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *datasetsRepo) SoftDelete(ctx context.Context, datasetID string, deletedAt int64) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE datasets SET deleted_at=? WHERE dataset_id=? AND deleted_at IS NULL`, deletedAt, datasetID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

type settingsRepo struct{ q querier }

func (r *settingsRepo) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := r.q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", store.ErrNotFound
	}
	return v, err
}

func (r *settingsRepo) Set(ctx context.Context, key, value string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
