package sqlite

import (
	"context"
	"database/sql"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type nodesRepo struct{ q querier }

const nodeCols = "node_id, credential_hash, hostname, description, agent_version, protocol_version, labels, gpus, status, first_seen, last_seen"

func scanNode(row interface{ Scan(...any) error }) (*store.Node, error) {
	var n store.Node
	err := row.Scan(&n.NodeID, &n.CredentialHash, &n.Hostname, &n.Description,
		&n.AgentVersion, &n.ProtocolVersion, &n.Labels, &n.GPUs, &n.Status,
		&n.FirstSeen, &n.LastSeen)
	return &n, err
}

func (r *nodesRepo) Create(ctx context.Context, n *store.Node) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO nodes (`+nodeCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		n.NodeID, n.CredentialHash, n.Hostname, n.Description, n.AgentVersion,
		n.ProtocolVersion, blob(n.Labels, "{}"), blob(n.GPUs, "[]"), n.Status,
		n.FirstSeen, n.LastSeen)
	return conflictOr(err)
}

func (r *nodesRepo) Get(ctx context.Context, nodeID string) (*store.Node, error) {
	n, err := scanNode(r.q.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE node_id=?`, nodeID))
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return n, err
}

func (r *nodesRepo) List(ctx context.Context) ([]*store.Node, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+nodeCols+` FROM nodes ORDER BY first_seen`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *nodesRepo) UpdateHeartbeat(ctx context.Context, nodeID string, lastSeen int64, status string) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE nodes SET last_seen=?, status=? WHERE node_id=?`, lastSeen, status, nodeID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func (r *nodesRepo) UpdateMeta(ctx context.Context, nodeID, description string, labels []byte) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE nodes SET description=?, labels=? WHERE node_id=?`,
		description, blob(labels, "{}"), nodeID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func (r *nodesRepo) SetStatus(ctx context.Context, nodeID, status string) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE nodes SET status=? WHERE node_id=?`, status, nodeID)
	if err != nil {
		return err
	}
	return affectedOne(res)
}

func blob(b []byte, def string) string {
	if len(b) == 0 {
		return def
	}
	return string(b)
}
