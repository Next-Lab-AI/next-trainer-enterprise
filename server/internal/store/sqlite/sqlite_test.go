package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUsers(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)

	if n, _ := db.Users().Count(ctx); n != 0 {
		t.Fatalf("empty db count = %d", n)
	}
	u := &store.User{ID: store.NewID(), Username: "admin", PasswordHash: "hash",
		Role: store.RoleAdmin, CreatedAt: 1, UpdatedAt: 1}
	if err := db.Users().Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	dup := &store.User{ID: store.NewID(), Username: "admin", PasswordHash: "h",
		Role: store.RoleUser, CreatedAt: 2, UpdatedAt: 2}
	if err := db.Users().Create(ctx, dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("dup username: %v", err)
	}
	got, err := db.Users().GetByUsername(ctx, "admin")
	if err != nil || got.ID != u.ID || got.Disabled {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := db.Users().Get(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	u.Role = store.RoleUser
	u.Disabled = true
	u.UpdatedAt = 3
	if err := db.Users().Update(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, _ = db.Users().Get(ctx, u.ID)
	if got.Role != store.RoleUser || !got.Disabled || got.UpdatedAt != 3 {
		t.Fatalf("update: %+v", got)
	}
	if err := db.Users().Update(ctx, &store.User{ID: "nope"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	list, _ := db.Users().List(ctx)
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}
}

func TestAccessTokens(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	u := &store.User{ID: store.NewID(), Username: "admin", PasswordHash: "h",
		Role: store.RoleAdmin, CreatedAt: 1, UpdatedAt: 1}
	db.Users().Create(ctx, u)

	tok := &store.AccessToken{ID: store.NewID(), TokenHash: "abc", Label: "ci",
		CreatedBy: u.ID, CreatedAt: 10}
	if err := db.AccessTokens().Create(ctx, tok); err != nil {
		t.Fatal(err)
	}
	got, err := db.AccessTokens().GetByHash(ctx, "abc")
	if err != nil || got.Label != "ci" || got.RevokedAt != nil {
		t.Fatalf("get: %v %+v", err, got)
	}
	if _, err := db.AccessTokens().GetByHash(ctx, "zzz"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := db.AccessTokens().Revoke(ctx, tok.ID, 20); err != nil {
		t.Fatal(err)
	}
	got, _ = db.AccessTokens().GetByHash(ctx, "abc")
	if got.RevokedAt == nil || *got.RevokedAt != 20 {
		t.Fatalf("revoked: %+v", got)
	}
	if err := db.AccessTokens().Revoke(ctx, tok.ID, 30); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("re-revoke: %v", err)
	}
	list, _ := db.AccessTokens().List(ctx)
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}
}

func TestNodes(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)

	n := &store.Node{NodeID: store.NewID(), CredentialHash: "ch", Hostname: "gpu0",
		AgentVersion: "0.1.0", ProtocolVersion: "1", Status: store.NodeStatusOnline,
		FirstSeen: 100, LastSeen: 100}
	if err := db.Nodes().Create(ctx, n); err != nil {
		t.Fatal(err)
	}
	got, err := db.Nodes().Get(ctx, n.NodeID)
	if err != nil || got.Hostname != "gpu0" || string(got.Labels) != "{}" || string(got.GPUs) != "[]" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if err := db.Nodes().UpdateHeartbeat(ctx, n.NodeID, 200, store.NodeStatusOnline); err != nil {
		t.Fatal(err)
	}
	if err := db.Nodes().UpdateMeta(ctx, n.NodeID, "rack a", []byte(`{"zone":"a"}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.Nodes().SetStatus(ctx, n.NodeID, store.NodeStatusRevoked); err != nil {
		t.Fatal(err)
	}
	got, _ = db.Nodes().Get(ctx, n.NodeID)
	if got.LastSeen != 200 || got.Description != "rack a" ||
		string(got.Labels) != `{"zone":"a"}` || got.Status != store.NodeStatusRevoked {
		t.Fatalf("updates: %+v", got)
	}
	if err := db.Nodes().SetStatus(ctx, "nope", store.NodeStatusOffline); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	list, _ := db.Nodes().List(ctx)
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}
}

func TestPools(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	u := &store.User{ID: store.NewID(), Username: "u1", PasswordHash: "h",
		Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1}
	db.Users().Create(ctx, u)
	n := &store.Node{NodeID: store.NewID(), CredentialHash: "c", Hostname: "h",
		Status: store.NodeStatusOnline, FirstSeen: 1, LastSeen: 1}
	db.Nodes().Create(ctx, n)

	p := &store.Pool{PoolID: store.NewID(), Name: "default", CreatedAt: 1}
	if err := db.Pools().Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := db.Pools().Create(ctx, &store.Pool{PoolID: store.NewID(), Name: "default"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("dup name: %v", err)
	}
	got, err := db.Pools().GetByName(ctx, "default")
	if err != nil || got.PoolID != p.PoolID {
		t.Fatalf("get by name: %v %+v", err, got)
	}

	db.Pools().AssignNode(ctx, n.NodeID, p.PoolID)
	db.Pools().AssignNode(ctx, n.NodeID, p.PoolID)
	db.Pools().GrantUser(ctx, u.ID, p.PoolID)
	db.Pools().GrantDataset(ctx, "ds1", p.PoolID)

	if ns, _ := db.Pools().NodesOf(ctx, p.PoolID); len(ns) != 1 || ns[0] != n.NodeID {
		t.Fatalf("nodes of: %v", ns)
	}
	if ps, _ := db.Pools().PoolsOfUser(ctx, u.ID); len(ps) != 1 || ps[0] != p.PoolID {
		t.Fatalf("pools of user: %v", ps)
	}
	if ps, _ := db.Pools().PoolsOfDataset(ctx, "ds1"); len(ps) != 1 {
		t.Fatalf("pools of dataset: %v", ps)
	}
	if us, _ := db.Pools().UsersOf(ctx, p.PoolID); len(us) != 1 {
		t.Fatalf("users of: %v", us)
	}

	if err := db.Pools().RevokeUser(ctx, u.ID, p.PoolID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pools().RevokeUser(ctx, u.ID, p.PoolID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("re-revoke: %v", err)
	}
	if err := db.Pools().Delete(ctx, p.PoolID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pools().Get(ctx, p.PoolID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}

func TestDatasets(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)

	d := &store.Dataset{DatasetID: store.NewID(), Name: "imgs", Version: "v1",
		Kind: store.DatasetKindDataset, Backend: store.BackendLocal, URI: "datasets/x",
		SHA256: "aa", SizeBytes: 42, CreatedBy: "u", CreatedAt: 1}
	if err := db.Datasets().Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := db.Datasets().Get(ctx, d.DatasetID)
	if err != nil || got.SizeBytes != 42 || got.DeletedAt != nil {
		t.Fatalf("get: %v %+v", err, got)
	}
	if err := db.Datasets().SoftDelete(ctx, d.DatasetID, 9); err != nil {
		t.Fatal(err)
	}
	live, _ := db.Datasets().List(ctx, false)
	all, _ := db.Datasets().List(ctx, true)
	if len(live) != 0 || len(all) != 1 {
		t.Fatalf("soft delete: live=%d all=%d", len(live), len(all))
	}
	if err := db.Datasets().SoftDelete(ctx, d.DatasetID, 10); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("re-delete: %v", err)
	}
}

func TestJobsEventsCommands(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	u := &store.User{ID: store.NewID(), Username: "u1", PasswordHash: "h",
		Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1}
	db.Users().Create(ctx, u)
	p := &store.Pool{PoolID: store.NewID(), Name: "default", CreatedAt: 1}
	db.Pools().Create(ctx, p)

	j := &store.Job{JobID: store.NewID(), UserID: u.ID, PoolID: p.PoolID,
		Engine: "sd-scripts", EngineVersion: "0.9.0", ConfigTOML: "x=1",
		Status: store.JobStatusQueued, CreatedAt: 1}
	if err := db.Jobs().CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	gpus := []byte(`[0,1]`)
	if err := db.Jobs().AssignJob(ctx, j.JobID, "node1", gpus); err != nil {
		t.Fatal(err)
	}
	start := int64(5)
	if err := db.Jobs().UpdateJobStatus(ctx, j.JobID, store.JobStatusRunning, nil, &start, nil); err != nil {
		t.Fatal(err)
	}
	exit := int64(0)
	end := int64(9)
	if err := db.Jobs().UpdateJobStatus(ctx, j.JobID, store.JobStatusFinished, &exit, nil, &end); err != nil {
		t.Fatal(err)
	}
	got, _ := db.Jobs().GetJob(ctx, j.JobID)
	if got.NodeID != "node1" || string(got.GPUs) != `[0,1]` || got.Status != store.JobStatusFinished ||
		got.ExitCode == nil || *got.ExitCode != 0 ||
		got.StartedAt == nil || *got.StartedAt != 5 || got.EndedAt == nil || *got.EndedAt != 9 {
		t.Fatalf("job: %+v", got)
	}
	list, _ := db.Jobs().ListJobs(ctx, store.JobFilter{Status: []string{store.JobStatusFinished}})
	if len(list) != 1 {
		t.Fatalf("filter: %d", len(list))
	}
	list, _ = db.Jobs().ListJobs(ctx, store.JobFilter{UserID: u.ID})
	if len(list) != 1 {
		t.Fatalf("by user: %d", len(list))
	}

	aseq := int64(7)
	el := int64(3)
	evs := []*store.Event{
		{NodeID: "node1", JobID: j.JobID, Type: "job.starting", Payload: []byte(`{"gpus":[0,1]}`), ServerTS: 5, AgentSeq: &aseq},
		{NodeID: "node1", JobID: j.JobID, Type: "job.finished", Payload: []byte(`{"exit_code":0}`), ServerTS: 9, ElapsedS: &el},
	}
	if err := db.Jobs().AppendEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	if evs[0].Seq == 0 || evs[1].Seq <= evs[0].Seq {
		t.Fatalf("seqs: %d %d", evs[0].Seq, evs[1].Seq)
	}
	gotEvs, err := db.Jobs().ListEvents(ctx, j.JobID, 0, 0)
	if err != nil || len(gotEvs) != 2 {
		t.Fatalf("events: %v %d", err, len(gotEvs))
	}
	if gotEvs[0].AgentSeq == nil || *gotEvs[0].AgentSeq != 7 || gotEvs[1].ElapsedS == nil || *gotEvs[1].ElapsedS != 3 {
		t.Fatalf("event fields: %+v %+v", gotEvs[0], gotEvs[1])
	}
	gotEvs, _ = db.Jobs().ListEvents(ctx, j.JobID, evs[0].Seq, 0)
	if len(gotEvs) != 1 {
		t.Fatalf("afterSeq: %d", len(gotEvs))
	}

	c := &store.Command{CommandID: store.NewID(), NodeID: "node1", Type: "job.start",
		Payload: []byte(`{"job_id":"x"}`), IssuedBy: u.ID, IssuedAt: 4}
	if err := db.Jobs().CreateCommand(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := db.Jobs().AckCommand(ctx, c.CommandID, 6); err != nil {
		t.Fatal(err)
	}
	cmds, _ := db.Jobs().ListCommands(ctx, "node1", 0)
	if len(cmds) != 1 || cmds[0].AckedAt == nil || *cmds[0].AckedAt != 6 {
		t.Fatalf("commands: %+v", cmds)
	}
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	if _, err := db.Settings().Get(ctx, "k"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := db.Settings().Set(ctx, "k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Settings().Set(ctx, "k", "v2"); err != nil {
		t.Fatal(err)
	}
	v, _ := db.Settings().Get(ctx, "k")
	if v != "v2" {
		t.Fatalf("get: %q", v)
	}
}

func TestTx(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)

	err := db.Tx(ctx, func(s store.Store) error {
		return s.Pools().Create(ctx, &store.Pool{PoolID: "p1", Name: "a", CreatedAt: 1})
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Tx(ctx, func(s store.Store) error {
		if err := s.Pools().Create(ctx, &store.Pool{PoolID: "p2", Name: "b", CreatedAt: 1}); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("tx err: %v", err)
	}
	if _, err := db.Pools().Get(ctx, "p2"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := db.Pools().Get(ctx, "p1"); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestFilePersistence(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := Open(dir + "/server.db")
	if err != nil {
		t.Fatal(err)
	}
	db.Pools().Create(ctx, &store.Pool{PoolID: "p1", Name: "persist", CreatedAt: 1})
	db.Close()

	db2, err := Open(dir + "/server.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	got, err := db2.Pools().Get(ctx, "p1")
	if err != nil || got.Name != "persist" {
		t.Fatalf("reopen: %v %+v", err, got)
	}
}
