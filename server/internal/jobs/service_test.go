package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/pools"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store/sqlite"
)

type fakeSender struct {
	mu       sync.Mutex
	online   map[string]bool
	commands []sentCommand
	failNext bool
}

type sentCommand struct {
	nodeID  string
	msgType string
	payload string
}

func newFakeSender() *fakeSender {
	return &fakeSender{online: make(map[string]bool)}
}

func (f *fakeSender) SendCommand(ctx context.Context, nodeID, msgType string, payload any, issuedBy string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return "", errors.New("send failed")
	}
	raw, _ := json.Marshal(payload)
	f.commands = append(f.commands, sentCommand{nodeID: nodeID, msgType: msgType, payload: string(raw)})
	return store.NewID(), nil
}

func (f *fakeSender) Online(nodeID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.online[nodeID]
}

func (f *fakeSender) setOnline(nodeID string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.online[nodeID] = on
}

type fixture struct {
	st     store.Store
	svc    *Service
	sender *fakeSender
	admin  *store.User
	user   *store.User
	pool   *store.Pool
	nodeID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	fx := &fixture{st: db, sender: newFakeSender()}

	fx.admin = &store.User{ID: store.NewID(), Username: "admin", PasswordHash: "h", Role: store.RoleAdmin, CreatedAt: 1, UpdatedAt: 1}
	fx.user = &store.User{ID: store.NewID(), Username: "u1", PasswordHash: "h", Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1}
	db.Users().Create(ctx, fx.admin)
	db.Users().Create(ctx, fx.user)
	fx.pool = &store.Pool{PoolID: store.NewID(), Name: "p1", CreatedAt: 1}
	db.Pools().Create(ctx, fx.pool)
	fx.nodeID = store.NewID()
	gpus, _ := json.Marshal([]protocol.GPUInfo{
		{Index: 0, UUID: "u0", Model: "GB10", Vendor: "nvidia"},
		{Index: 1, UUID: "u1", Model: "GB10", Vendor: "nvidia"},
	})
	db.Nodes().Create(ctx, &store.Node{NodeID: fx.nodeID, CredentialHash: "c", Hostname: "h",
		GPUs: gpus, Status: store.NodeStatusOnline, FirstSeen: 1, LastSeen: 1})
	db.Pools().AssignNode(ctx, fx.nodeID, fx.pool.PoolID)
	fx.sender.setOnline(fx.nodeID, true)

	fx.svc = New(db, pools.New(db), fx.sender, Config{
		LogDir:           t.TempDir(),
		HTTPBase:         "https://server.local",
		DispatchInterval: 50 * time.Millisecond,
	})
	return fx
}

func (fx *fixture) heartbeat(free ...int) {
	fx.svc.OnHeartbeat(fx.nodeID, protocol.Heartbeat{FreeGPUs: free})
}

func TestSubmitAuthzAndQueue(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	_, err := fx.svc.Submit(ctx, fx.user, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ungranted submit: %v", err)
	}
	fx.st.Pools().GrantUser(ctx, fx.user.ID, fx.pool.PoolID)

	_, err = fx.svc.Submit(ctx, fx.user, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "nope"})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("bad engine: %v", err)
	}

	d := &store.Dataset{DatasetID: store.NewID(), Name: "imgs", Kind: store.DatasetKindDataset,
		Backend: store.BackendLocal, URI: "datasets/x", SHA256: "aa", SizeBytes: 1, CreatedBy: fx.admin.ID, CreatedAt: 1}
	fx.st.Datasets().Create(ctx, d)

	req := SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts", DatasetIDs: []string{d.DatasetID},
		Params: map[string]any{"max_train_epochs": 2}, GPUs: 1}
	if _, err := fx.svc.Submit(ctx, fx.user, req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("dataset not granted: %v", err)
	}
	fx.st.Pools().GrantDataset(ctx, d.DatasetID, fx.pool.PoolID)

	job, err := fx.svc.Submit(ctx, fx.user, req)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != store.JobStatusQueued {
		t.Fatalf("status: %s", job.Status)
	}
	if !strings.Contains(job.ConfigTOML, "max_train_epochs = 2") {
		t.Fatalf("config: %s", job.ConfigTOML)
	}
	var assets []protocol.Asset
	json.Unmarshal(job.Assets, &assets)
	if len(assets) != 1 || assets[0].URI != "https://server.local/datasets/"+d.DatasetID {
		t.Fatalf("assets: %v", assets)
	}
}

func TestDispatchAndLifecycle(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.heartbeat(0, 1)

	job, err := fx.svc.Submit(ctx, fx.admin, SubmitRequest{
		PoolID: fx.pool.PoolID, Engine: "sd-scripts", GPUs: 2})
	if err != nil {
		t.Fatal(err)
	}

	fx.svc.dispatchAll(ctx)
	fx.sender.mu.Lock()
	if len(fx.sender.commands) != 1 {
		fx.sender.mu.Unlock()
		t.Fatalf("commands: %d", len(fx.sender.commands))
	}
	cmd := fx.sender.commands[0]
	fx.sender.mu.Unlock()
	if cmd.nodeID != fx.nodeID || cmd.msgType != protocol.TypeJobStart {
		t.Fatalf("cmd: %+v", cmd)
	}
	var js protocol.JobStart
	json.Unmarshal([]byte(cmd.payload), &js)
	if js.JobID != job.JobID || js.Lane != protocol.LaneCompute || len(js.GPUs) != 2 {
		t.Fatalf("job.start: %+v", js)
	}

	got, _ := fx.st.Jobs().GetJob(ctx, job.JobID)
	if got.NodeID != fx.nodeID || string(got.GPUs) != "[0,1]" {
		t.Fatalf("assigned: %+v", got)
	}

	fx.svc.dispatchAll(ctx)
	fx.sender.mu.Lock()
	n := len(fx.sender.commands)
	fx.sender.mu.Unlock()
	if n != 1 {
		t.Fatal("double dispatch")
	}

	fx.svc.OnEvent(fx.nodeID, 1, protocol.Event{EventType: protocol.EventJobCreated, JobID: job.JobID})
	fx.svc.OnEvent(fx.nodeID, 2, protocol.Event{EventType: protocol.EventJobStarting, JobID: job.JobID})
	got, _ = fx.st.Jobs().GetJob(ctx, job.JobID)
	if got.Status != store.JobStatusRunning || got.StartedAt == nil {
		t.Fatalf("running: %+v", got)
	}

	fx.svc.OnEvent(fx.nodeID, 3, protocol.Event{EventType: protocol.EventJobFinished, JobID: job.JobID,
		Data: map[string]any{"exit_code": float64(0)}})
	got, _ = fx.st.Jobs().GetJob(ctx, job.JobID)
	if got.Status != store.JobStatusFinished || got.ExitCode == nil || got.EndedAt == nil {
		t.Fatalf("finished: %+v", got)
	}

	evs, _ := fx.st.Jobs().ListEvents(ctx, job.JobID, 0, 0)
	if len(evs) != 3 {
		t.Fatalf("events: %d", len(evs))
	}

	fx.svc.OnEvent(fx.nodeID, 3, protocol.Event{EventType: protocol.EventJobFinished, JobID: job.JobID})
	evs, _ = fx.st.Jobs().ListEvents(ctx, job.JobID, 0, 0)
	if len(evs) != 3 {
		t.Fatalf("dup event inserted: %d", len(evs))
	}
}

func TestDispatchWaitsForFreeGPUs(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.heartbeat(0)

	if _, err := fx.svc.Submit(ctx, fx.admin, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts", GPUs: 2}); err != nil {
		t.Fatal(err)
	}
	fx.svc.dispatchAll(ctx)
	fx.sender.mu.Lock()
	n := len(fx.sender.commands)
	fx.sender.mu.Unlock()
	if n != 0 {
		t.Fatal("dispatched without enough free gpus")
	}

	fx.heartbeat(0, 1)
	fx.svc.dispatchAll(ctx)
	fx.sender.mu.Lock()
	n = len(fx.sender.commands)
	fx.sender.mu.Unlock()
	if n != 1 {
		t.Fatal("not dispatched after gpus freed")
	}
}

func TestGPUModelMatch(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.heartbeat(0, 1)

	if _, err := fx.svc.Submit(ctx, fx.admin, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts",
		GPUs: 1, GPUModel: "H100"}); err != nil {
		t.Fatal(err)
	}
	fx.svc.dispatchAll(ctx)
	fx.sender.mu.Lock()
	n := len(fx.sender.commands)
	fx.sender.mu.Unlock()
	if n != 0 {
		t.Fatal("dispatched despite model mismatch")
	}
}

func TestStopKillAuthz(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.heartbeat(0, 1)
	fx.st.Pools().GrantUser(ctx, fx.user.ID, fx.pool.PoolID)

	job, _ := fx.svc.Submit(ctx, fx.user, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts", GPUs: 1})
	fx.svc.dispatchAll(ctx)
	fx.svc.OnEvent(fx.nodeID, 1, protocol.Event{EventType: protocol.EventJobStarting, JobID: job.JobID})

	other := &store.User{ID: store.NewID(), Username: "u2", PasswordHash: "h", Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1}
	fx.st.Users().Create(ctx, other)

	if err := fx.svc.Stop(ctx, other, job.JobID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other user stop: %v", err)
	}
	if err := fx.svc.Stop(ctx, fx.user, job.JobID, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("user preempt: %v", err)
	}
	if err := fx.svc.Stop(ctx, fx.user, job.JobID, false); err != nil {
		t.Fatal(err)
	}
	fx.sender.mu.Lock()
	last := fx.sender.commands[len(fx.sender.commands)-1]
	fx.sender.mu.Unlock()
	if last.msgType != protocol.TypeJobStop {
		t.Fatalf("stop cmd: %+v", last)
	}

	if err := fx.svc.Kill(ctx, fx.admin, job.JobID); err != nil {
		t.Fatal(err)
	}
	fx.sender.mu.Lock()
	last = fx.sender.commands[len(fx.sender.commands)-1]
	fx.sender.mu.Unlock()
	if last.msgType != protocol.TypeJobKill {
		t.Fatalf("kill cmd: %+v", last)
	}

	fx.svc.OnEvent(fx.nodeID, 2, protocol.Event{EventType: protocol.EventJobTerminated, JobID: job.JobID,
		Data: map[string]any{"reason": "kill"}})
	if err := fx.svc.Stop(ctx, fx.admin, job.JobID, false); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stop terminal: %v", err)
	}
}

func TestListVisibility(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.st.Pools().GrantUser(ctx, fx.user.ID, fx.pool.PoolID)
	fx.svc.Submit(ctx, fx.admin, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts"})
	fx.svc.Submit(ctx, fx.user, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts"})

	adminList, _ := fx.svc.List(ctx, fx.admin, store.JobFilter{})
	if len(adminList) != 2 {
		t.Fatalf("admin list: %d", len(adminList))
	}
	userList, _ := fx.svc.List(ctx, fx.user, store.JobFilter{})
	if len(userList) != 2 {
		t.Fatalf("user list (own + granted pool): %d", len(userList))
	}

	other := &store.User{ID: store.NewID(), Username: "u2", PasswordHash: "h", Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1}
	fx.st.Users().Create(ctx, other)
	otherList, _ := fx.svc.List(ctx, other, store.JobFilter{})
	if len(otherList) != 0 {
		t.Fatalf("other list: %d", len(otherList))
	}
}

func TestLogIngest(t *testing.T) {
	fx := newFixture(t)
	line1 := json.RawMessage(`{"ts":"2026-10-12T00:00:00Z","level":"info","source":"engine","node_id":"n","job_id":"j1","msg":"step 1"}`)
	line2 := json.RawMessage(`{"ts":"2026-10-12T00:00:01Z","level":"info","source":"engine","node_id":"n","job_id":"j1","msg":"step 2"}`)
	fx.svc.OnLog(fx.nodeID, 5, protocol.Log{Lines: []json.RawMessage{line1, line2}})

	data, err := os.ReadFile(filepath.Join(fx.svc.cfg.LogDir, "jobs", "j1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(strings.TrimSpace(string(data)), "\n")) != 2 {
		t.Fatalf("log lines: %q", data)
	}
}

func TestRebuildQueues(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	j1, _ := fx.svc.Submit(ctx, fx.admin, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts"})
	fx.svc.Submit(ctx, fx.admin, SubmitRequest{PoolID: fx.pool.PoolID, Engine: "sd-scripts"})

	fx2svc := New(fx.st, pools.New(fx.st), fx.sender, Config{LogDir: t.TempDir()})
	if err := fx2svc.RebuildQueues(ctx); err != nil {
		t.Fatal(err)
	}
	fx2svc.mu.Lock()
	n := len(fx2svc.queues[fx.pool.PoolID])
	first := fx2svc.queues[fx.pool.PoolID][0]
	fx2svc.mu.Unlock()
	if n != 2 || first != j1.JobID {
		t.Fatalf("rebuilt queue: %d first=%s want %s", n, first, j1.JobID)
	}
}
