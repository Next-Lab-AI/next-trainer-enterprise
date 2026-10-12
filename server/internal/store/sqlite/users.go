package sqlite

import (
	"context"
	"database/sql"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type usersRepo struct{ q querier }

const userCols = "id, username, password_hash, role, disabled, created_at, updated_at"

func scanUser(row interface{ Scan(...any) error }) (*store.User, error) {
	var u store.User
	var disabled int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &disabled, &u.CreatedAt, &u.UpdatedAt)
	u.Disabled = disabled != 0
	return &u, err
}

func (r *usersRepo) Create(ctx context.Context, u *store.User) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO users (`+userCols+`) VALUES (?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.PasswordHash, u.Role, boolInt(u.Disabled), u.CreatedAt, u.UpdatedAt)
	return conflictOr(err)
}

func (r *usersRepo) Get(ctx context.Context, id string) (*store.User, error) {
	u, err := scanUser(r.q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return u, err
}

func (r *usersRepo) GetByUsername(ctx context.Context, username string) (*store.User, error) {
	u, err := scanUser(r.q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username=?`, username))
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return u, err
}

func (r *usersRepo) List(ctx context.Context) ([]*store.User, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at, username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *usersRepo) Update(ctx context.Context, u *store.User) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE users SET password_hash=?, role=?, disabled=?, updated_at=? WHERE id=?`,
		u.PasswordHash, u.Role, boolInt(u.Disabled), u.UpdatedAt, u.ID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func (r *usersRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func affectedOne(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}
