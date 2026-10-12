package sqlite

import (
	"context"
	"database/sql"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type tokensRepo struct{ q querier }

const tokenCols = "id, token_hash, label, created_by, created_at, expires_at, revoked_at"

func scanToken(row interface{ Scan(...any) error }) (*store.AccessToken, error) {
	var t store.AccessToken
	var exp, rev sql.NullInt64
	err := row.Scan(&t.ID, &t.TokenHash, &t.Label, &t.CreatedBy, &t.CreatedAt, &exp, &rev)
	t.ExpiresAt = intPtr(exp)
	t.RevokedAt = intPtr(rev)
	return &t, err
}

func (r *tokensRepo) Create(ctx context.Context, t *store.AccessToken) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO access_tokens (`+tokenCols+`) VALUES (?,?,?,?,?,?,?)`,
		t.ID, t.TokenHash, t.Label, t.CreatedBy, t.CreatedAt, nullInt(t.ExpiresAt), nullInt(t.RevokedAt))
	return conflictOr(err)
}

func (r *tokensRepo) GetByHash(ctx context.Context, hash string) (*store.AccessToken, error) {
	t, err := scanToken(r.q.QueryRowContext(ctx, `SELECT `+tokenCols+` FROM access_tokens WHERE token_hash=?`, hash))
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return t, err
}

func (r *tokensRepo) List(ctx context.Context) ([]*store.AccessToken, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+tokenCols+` FROM access_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.AccessToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *tokensRepo) Revoke(ctx context.Context, id string, revokedAt int64) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE access_tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, revokedAt, id)
	if err != nil {
		return err
	}
	return affectedOne(res)
}
