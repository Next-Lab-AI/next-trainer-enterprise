package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type jobsRepo struct{ q querier }

const jobCols = "job_id, user_id, pool_id, node_id, engine, engine_version, config_toml, assets, gpus, status, exit_code, request, created_at, started_at, ended_at"

func scanJob(row interface{ Scan(...any) error }) (*store.Job, error) {
	var j store.Job
	var exit sql.NullInt64
	var started, ended sql.NullInt64
	err := row.Scan(&j.JobID, &j.UserID, &j.PoolID, &j.NodeID, &j.Engine, &j.EngineVersion,
		&j.ConfigTOML, &j.Assets, &j.GPUs, &j.Status, &exit, &j.Request, &j.CreatedAt, &started, &ended)
	j.ExitCode = intPtr(exit)
	j.StartedAt = intPtr(started)
	j.EndedAt = intPtr(ended)
	return &j, err
}

func (r *jobsRepo) CreateJob(ctx context.Context, j *store.Job) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO jobs (`+jobCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.JobID, j.UserID, j.PoolID, j.NodeID, j.Engine, j.EngineVersion,
		j.ConfigTOML, blob(j.Assets, "[]"), blob(j.GPUs, "[]"), j.Status,
		nullInt(j.ExitCode), blob(j.Request, "{}"), j.CreatedAt, nullInt(j.StartedAt), nullInt(j.EndedAt))
	return conflictOr(err)
}

func (r *jobsRepo) GetJob(ctx context.Context, jobID string) (*store.Job, error) {
	j, err := scanJob(r.q.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE job_id=?`, jobID))
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return j, err
}

func (r *jobsRepo) ListJobs(ctx context.Context, f store.JobFilter) ([]*store.Job, error) {
	q := `SELECT ` + jobCols + ` FROM jobs WHERE 1=1`
	var args []any
	if f.UserID != "" {
		q += ` AND user_id=?`
		args = append(args, f.UserID)
	}
	if f.PoolID != "" {
		q += ` AND pool_id=?`
		args = append(args, f.PoolID)
	}
	if len(f.Status) > 0 {
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(f.Status)), ",") + `)`
		for _, s := range f.Status {
			args = append(args, s)
		}
	}
	q += ` ORDER BY created_at DESC, rowid DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
		if f.Offset > 0 {
			q += ` OFFSET ?`
			args = append(args, f.Offset)
		}
	}
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (r *jobsRepo) AssignJob(ctx context.Context, jobID, nodeID string, gpus []byte) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE jobs SET node_id=?, gpus=? WHERE job_id=?`, nodeID, blob(gpus, "[]"), jobID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func (r *jobsRepo) UpdateJobStatus(ctx context.Context, jobID, status string, exitCode *int64, startedAt, endedAt *int64) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE jobs SET status=?,
			exit_code = COALESCE(?, exit_code),
			started_at = COALESCE(?, started_at),
			ended_at = COALESCE(?, ended_at)
		 WHERE job_id=?`,
		status, nullInt(exitCode), nullInt(startedAt), nullInt(endedAt), jobID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func (r *jobsRepo) AppendEvents(ctx context.Context, events []*store.Event) error {
	for _, e := range events {
		res, err := r.q.ExecContext(ctx,
			`INSERT OR IGNORE INTO events (node_id, job_id, type, payload, server_ts, agent_seq, elapsed_s, gap)
			 VALUES (?,?,?,?,?,?,?,?)`,
			e.NodeID, e.JobID, e.Type, blob(e.Payload, "{}"), e.ServerTS,
			nullInt(e.AgentSeq), nullInt(e.ElapsedS), boolInt(e.Gap))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			e.Seq = -1
			continue
		}
		seq, err := res.LastInsertId()
		if err != nil {
			return err
		}
		e.Seq = seq
	}
	return nil
}

func (r *jobsRepo) ListEvents(ctx context.Context, jobID string, afterSeq int64, limit int) ([]*store.Event, error) {
	q := `SELECT seq, node_id, job_id, type, payload, server_ts, agent_seq, elapsed_s, gap
		FROM events WHERE job_id=? AND seq>? ORDER BY seq`
	var args []any
	args = append(args, jobID, afterSeq)
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.Event
	for rows.Next() {
		var e store.Event
		var aseq, el sql.NullInt64
		var gap int
		if err := rows.Scan(&e.Seq, &e.NodeID, &e.JobID, &e.Type, &e.Payload,
			&e.ServerTS, &aseq, &el, &gap); err != nil {
			return nil, err
		}
		e.AgentSeq = intPtr(aseq)
		e.ElapsedS = intPtr(el)
		e.Gap = gap != 0
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (r *jobsRepo) CreateCommand(ctx context.Context, c *store.Command) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO commands (command_id, node_id, type, payload, issued_by, issued_at, acked_at)
		 VALUES (?,?,?,?,?,?,?)`,
		c.CommandID, c.NodeID, c.Type, blob(c.Payload, "{}"), c.IssuedBy, c.IssuedAt, nullInt(c.AckedAt))
	return conflictOr(err)
}

func (r *jobsRepo) AckCommand(ctx context.Context, commandID string, ackedAt int64) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE commands SET acked_at=? WHERE command_id=?`, ackedAt, commandID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func (r *jobsRepo) ListCommands(ctx context.Context, nodeID string, limit int) ([]*store.Command, error) {
	q := `SELECT command_id, node_id, type, payload, issued_by, issued_at, acked_at
		FROM commands WHERE node_id=? ORDER BY issued_at DESC`
	var args []any
	args = append(args, nodeID)
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.Command
	for rows.Next() {
		var c store.Command
		var ack sql.NullInt64
		if err := rows.Scan(&c.CommandID, &c.NodeID, &c.Type, &c.Payload,
			&c.IssuedBy, &c.IssuedAt, &ack); err != nil {
			return nil, err
		}
		c.AckedAt = intPtr(ack)
		out = append(out, &c)
	}
	return out, rows.Err()
}
