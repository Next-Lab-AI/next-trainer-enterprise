package ws

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/registry"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store/sqlite"
)

type fakeHandler struct {
	mu         sync.Mutex
	heartbeats []protocol.Heartbeat
	events     []protocol.Event
	offline    []string
}

func (f *fakeHandler) OnEvent(nodeID string, seq int64, e protocol.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}
func (f *fakeHandler) OnMetrics(string, int64, protocol.Metrics)         {}
func (f *fakeHandler) OnLog(string, int64, protocol.Log)                 {}
func (f *fakeHandler) OnAssetReady(string, int64, protocol.AssetReady)   {}
func (f *fakeHandler) OnAssetFailed(string, int64, protocol.AssetFailed) {}
func (f *fakeHandler) OnFile(string, int64, protocol.FileChunk)          {}
func (f *fakeHandler) OnHeartbeat(nodeID string, hb protocol.Heartbeat) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeats = append(f.heartbeats, hb)
}
func (f *fakeHandler) OnNodeOnline(nodeID string) {}
func (f *fakeHandler) OnNodeOffline(nodeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = append(f.offline, nodeID)
}

type fixture struct {
	hub *Hub
	st  store.Store
	reg *registry.Registry
	svc *auth.Service
	url string
	fh  *fakeHandler
	srv *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := auth.NewService(db, time.Hour)
	reg := registry.New(db, svc)
	fh := &fakeHandler{}
	hub := NewHub(reg, db, Config{HeartbeatIntervalS: 1})
	hub.SetHandler(fh)
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close)
	return &fixture{hub: hub, st: db, reg: reg, svc: svc,
		url: "ws" + strings.TrimPrefix(srv.URL, "http"), fh: fh, srv: srv}
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
	return c
}

func sendHello(t *testing.T, c *websocket.Conn, h protocol.Hello) {
	t.Helper()
	f, err := protocol.NewFrame(protocol.TypeHello, "", 1, h)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := protocol.Encode(f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func readWelcome(t *testing.T, c *websocket.Conn) protocol.Welcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := protocol.Decode(raw)
	if err != nil || f.Type != protocol.TypeWelcome {
		t.Fatalf("want welcome got %v %v", f.Type, err)
	}
	w, err := protocol.DecodePayload[protocol.Welcome](f)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func expectClose(t *testing.T, c *websocket.Conn, reason string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	var ce websocket.CloseError
	if !errors.As(err, &ce) {
		t.Fatalf("want close error, got %v", err)
	}
	if ce.Reason != reason {
		t.Fatalf("close reason %q, want %q", ce.Reason, reason)
	}
}

func helloPayload() protocol.Hello {
	return protocol.Hello{
		AgentVersion: "0.1.0", Hostname: "gpu0",
		GPUs:   []protocol.GPUInfo{{Index: 0, UUID: "u0", Model: "GB10", Vendor: "nvidia", MemTotalMB: 128000}},
		CPU:    "arm64",
		MemMB:  120000,
		Labels: map[string]string{"zone": "a"},
	}
}

func TestWindowClosedRejected(t *testing.T) {
	fx := newFixture(t)
	c := dial(t, fx.url)
	sendHello(t, c, helloPayload())
	expectClose(t, c, protocol.CloseWindowClosed)
}

func TestRegisterAndResume(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	if err := fx.svc.SetRegistrationOpen(ctx, true); err != nil {
		t.Fatal(err)
	}

	c := dial(t, fx.url)
	sendHello(t, c, helloPayload())
	w := readWelcome(t, c)
	if w.NodeID == "" || w.Credential == "" || w.SessionToken == "" || w.HeartbeatIntervalS != 1 {
		t.Fatalf("welcome: %+v", w)
	}

	n, err := fx.st.Nodes().Get(ctx, w.NodeID)
	if err != nil || n.Status != store.NodeStatusOnline || n.Hostname != "gpu0" {
		t.Fatalf("node: %v %+v", err, n)
	}
	pools, _ := fx.st.Pools().PoolsOfNode(ctx, w.NodeID)
	if len(pools) != 1 {
		t.Fatalf("pools: %v", pools)
	}
	pool, _ := fx.st.Pools().Get(ctx, pools[0])
	if pool.Name != "default" || !pool.AutoCreated {
		t.Fatalf("pool: %+v", pool)
	}

	c.Close(websocket.StatusNormalClosure, "")
	c2 := dial(t, fx.url)
	h := helloPayload()
	h.NodeID = w.NodeID
	h.Credential = w.Credential
	sendHello(t, c2, h)
	w2 := readWelcome(t, c2)
	if w2.NodeID != w.NodeID || w2.Credential != "" {
		t.Fatalf("resume welcome: %+v", w2)
	}

	c3 := dial(t, fx.url)
	h.Credential = "wrong"
	sendHello(t, c3, h)
	expectClose(t, c3, protocol.CloseBadToken)
}

func TestAccessTokenRegister(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	u, _ := fx.svc.CreateFirstAdmin(ctx, "admin", "pw")
	plain, _, err := fx.svc.IssueAccessToken(ctx, "node", u.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := dial(t, fx.url)
	h := helloPayload()
	h.AccessToken = plain
	sendHello(t, c, h)
	w := readWelcome(t, c)
	if w.NodeID == "" {
		t.Fatal("no node id")
	}
}

func TestBadAccessToken(t *testing.T) {
	fx := newFixture(t)
	c := dial(t, fx.url)
	h := helloPayload()
	h.AccessToken = "bogus"
	sendHello(t, c, h)
	expectClose(t, c, protocol.CloseBadToken)
}

func TestRevokedNode(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.svc.SetRegistrationOpen(ctx, true)
	c := dial(t, fx.url)
	sendHello(t, c, helloPayload())
	w := readWelcome(t, c)
	if err := fx.reg.Revoke(ctx, w.NodeID); err != nil {
		t.Fatal(err)
	}
	c.Close(websocket.StatusNormalClosure, "")

	c2 := dial(t, fx.url)
	h := helloPayload()
	h.NodeID = w.NodeID
	h.Credential = w.Credential
	sendHello(t, c2, h)
	expectClose(t, c2, protocol.CloseRevoked)
}

func TestHeartbeatAndOffline(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.svc.SetRegistrationOpen(ctx, true)
	c := dial(t, fx.url)
	sendHello(t, c, helloPayload())
	w := readWelcome(t, c)

	before, _ := fx.st.Nodes().Get(ctx, w.NodeID)
	time.Sleep(1100 * time.Millisecond)

	f, _ := protocol.NewFrame(protocol.TypeHeartbeat, "", 2, protocol.Heartbeat{
		Jobs:     []protocol.JobStatusFact{{JobID: "j1", Status: "RUNNING"}},
		FreeGPUs: []int{0},
	})
	data, _ := protocol.Encode(f)
	wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	err := c.Write(wctx, websocket.MessageText, data)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	after, _ := fx.st.Nodes().Get(ctx, w.NodeID)
	if after.LastSeen <= before.LastSeen {
		t.Fatalf("last_seen not updated: %d -> %d", before.LastSeen, after.LastSeen)
	}
	fx.fh.mu.Lock()
	hbCount := len(fx.fh.heartbeats)
	fx.fh.mu.Unlock()
	if hbCount != 1 {
		t.Fatalf("heartbeats: %d", hbCount)
	}

	c.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, _ := fx.st.Nodes().Get(ctx, w.NodeID)
		if n.Status == store.NodeStatusOffline {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("node not marked offline after disconnect")
}

func TestSendCommandAudit(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.svc.SetRegistrationOpen(ctx, true)
	c := dial(t, fx.url)
	sendHello(t, c, helloPayload())
	w := readWelcome(t, c)

	cmdID, err := fx.hub.SendCommand(ctx, w.NodeID, protocol.TypeEnvGC, protocol.EnvGC{OlderThanDays: 7}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, raw, err := c.Read(rctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := protocol.Decode(raw)
	if err != nil || f.Type != protocol.TypeEnvGC || f.CommandID != cmdID {
		t.Fatalf("frame: %+v %v", f, err)
	}
	gc, _ := protocol.DecodePayload[protocol.EnvGC](f)
	if gc.OlderThanDays != 7 {
		t.Fatalf("payload: %+v", gc)
	}
	cmds, _ := fx.st.Jobs().ListCommands(ctx, w.NodeID, 0)
	if len(cmds) != 1 || cmds[0].CommandID != cmdID || cmds[0].IssuedBy != "admin" {
		t.Fatalf("commands: %+v", cmds)
	}

	if _, err := fx.hub.SendCommand(ctx, "ghost", protocol.TypeEnvGC, protocol.EnvGC{}, ""); err == nil {
		t.Fatal("expected error for offline node")
	}
}

func TestSessionTokenAuth(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.svc.SetRegistrationOpen(ctx, true)
	c := dial(t, fx.url)
	sendHello(t, c, helloPayload())
	w := readWelcome(t, c)

	nodeID, ok := fx.hub.AuthenticateSession(w.SessionToken)
	if !ok || nodeID != w.NodeID {
		t.Fatalf("session auth: %v %q", ok, nodeID)
	}
	if _, ok := fx.hub.AuthenticateSession("bogus"); ok {
		t.Fatal("bogus token accepted")
	}
	c.Close(websocket.StatusNormalClosure, "")
	time.Sleep(200 * time.Millisecond)
	if _, ok := fx.hub.AuthenticateSession(w.SessionToken); ok {
		t.Fatal("token still valid after disconnect")
	}
}

func TestDuplicateConnectionReplaced(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	fx.svc.SetRegistrationOpen(ctx, true)
	c1 := dial(t, fx.url)
	sendHello(t, c1, helloPayload())
	w := readWelcome(t, c1)

	c2 := dial(t, fx.url)
	h := helloPayload()
	h.NodeID = w.NodeID
	h.Credential = w.Credential
	sendHello(t, c2, h)
	readWelcome(t, c2)

	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, _, err := c1.Read(rctx)
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Reason != "replaced" {
		t.Fatalf("old conn: %v", err)
	}
	if !fx.hub.Online(w.NodeID) {
		t.Fatal("node should be online via new conn")
	}
}
