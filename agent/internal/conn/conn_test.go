package conn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

type fakeServer struct {
	t *testing.T

	mu           sync.Mutex
	received     []protocol.Frame
	welcome      protocol.Welcome
	closeWith    string
	afterWelcome func(ctx context.Context, ws *websocket.Conn)
	active       *websocket.Conn
}

func (s *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.t.Logf("accept: %v", err)
		return
	}
	defer ws.CloseNow()

	s.mu.Lock()
	s.active = ws
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active = nil
		s.mu.Unlock()
	}()

	_, raw, err := ws.Read(r.Context())
	if err != nil {
		return
	}
	f, err := protocol.Decode(raw)
	if err != nil || f.Type != protocol.TypeHello {
		s.t.Errorf("want hello, got %q (%v)", f.Type, err)
		return
	}
	s.mu.Lock()
	s.received = append(s.received, f)
	s.mu.Unlock()

	if s.closeWith != "" {
		ws.Close(websocket.StatusPolicyViolation, s.closeWith)
		return
	}

	wf, _ := protocol.NewFrame(protocol.TypeWelcome, "", 0, s.welcome)
	data, _ := protocol.Encode(wf)
	if err := ws.Write(r.Context(), websocket.MessageText, data); err != nil {
		return
	}
	if s.afterWelcome != nil {
		go s.afterWelcome(r.Context(), ws)
	}

	for {
		_, raw, err := ws.Read(r.Context())
		if err != nil {
			return
		}
		f, err := protocol.Decode(raw)
		if err != nil {
			continue
		}
		s.mu.Lock()
		s.received = append(s.received, f)
		s.mu.Unlock()
	}
}

func (s *fakeServer) frames() []protocol.Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.Frame(nil), s.received...)
}

func (s *fakeServer) dropActive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		s.active.Close(websocket.StatusAbnormalClosure, "simulated network loss")
	}
}

func startServer(t *testing.T, s *fakeServer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + srv.URL[len("http"):]
}

func testHooks(s *fakeServer) Hooks {
	return Hooks{
		Hello: func() protocol.Hello {
			return protocol.Hello{AgentVersion: "test", Hostname: "h", GPUs: []protocol.GPUInfo{}}
		},
		HeartbeatFacts: func() protocol.Heartbeat {
			return protocol.Heartbeat{Jobs: []protocol.JobStatusFact{}, FreeGPUs: []int{0}}
		},
	}
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting: %s", msg)
}

func TestHandshakeAndHeartbeat(t *testing.T) {
	s := &fakeServer{t: t, welcome: protocol.Welcome{
		NodeID: "n1", Credential: "cred", SessionToken: "sess", HeartbeatIntervalS: 1,
	}}
	srv := startServer(t, s)

	var welcome protocol.Welcome
	var token string
	hooks := testHooks(s)
	hooks.OnWelcome = func(w protocol.Welcome) { welcome = w }
	hooks.OnSessionToken = func(tok string) { token = tok }

	c := New(Config{ServerURL: wsURL(srv)}, hooks, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	waitFor(t, func() bool { return welcome.NodeID == "n1" }, "welcome")
	if token != "sess" {
		t.Fatalf("session token = %q", token)
	}
	waitFor(t, func() bool {
		for _, f := range s.frames() {
			if f.Type == protocol.TypeHeartbeat {
				return true
			}
		}
		return false
	}, "heartbeat")

	for _, f := range s.frames() {
		if f.Type == protocol.TypeHeartbeat {
			var hb protocol.Heartbeat
			if err := json.Unmarshal(f.Payload, &hb); err != nil {
				t.Fatalf("heartbeat payload: %v", err)
			}
			if len(hb.FreeGPUs) != 1 {
				t.Fatalf("heartbeat facts: %+v", hb)
			}
		}
	}
}

func TestHandshakeRejected(t *testing.T) {
	s := &fakeServer{t: t, closeWith: protocol.CloseBadToken}
	srv := startServer(t, s)

	c := New(Config{ServerURL: wsURL(srv), Backoff: Backoff{Initial: 20 * time.Millisecond, Max: 50 * time.Millisecond}}, testHooks(s), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := c.Run(ctx); err == nil {
		t.Fatal("want ctx error after retries")
	}
	if len(s.frames()) < 2 {
		t.Fatalf("expected retries, got %d hello(s)", len(s.frames()))
	}
}

func TestReconnectSeqContinuityAndReplay(t *testing.T) {
	s := &fakeServer{t: t, welcome: protocol.Welcome{
		NodeID: "n1", Credential: "cred", SessionToken: "sess", HeartbeatIntervalS: 3600,
	}}
	srv := startServer(t, s)

	buf := &SliceBuffer{}
	var persisted []int64
	var mu sync.Mutex
	hooks := testHooks(s)
	hooks.PersistSeq = func(seq int64) {
		mu.Lock()
		persisted = append(persisted, seq)
		mu.Unlock()
	}
	c := New(Config{ServerURL: wsURL(srv), Backoff: Backoff{Initial: 20 * time.Millisecond, Max: 50 * time.Millisecond}, Buffer: buf}, hooks, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	waitFor(t, func() bool {
		for _, f := range s.frames() {
			if f.Type == protocol.TypeHello {
				return true
			}
		}
		return false
	}, "first hello")

	s.dropActive()
	waitFor(t, func() bool { return !c.Connected() }, "disconnect detected")

	if err := c.Send(ctx, protocol.TypeEvent, protocol.Event{EventType: protocol.EventJobProgress, JobID: "j1"}); err != nil {
		t.Fatalf("buffered send: %v", err)
	}
	if buf.Len() != 1 {
		t.Fatalf("buffer len = %d", buf.Len())
	}

	waitFor(t, func() bool {
		for _, f := range s.frames() {
			if f.Type == protocol.TypeEvent {
				return true
			}
		}
		return false
	}, "replayed event")

	mu.Lock()
	for i := 1; i < len(persisted); i++ {
		if persisted[i] <= persisted[i-1] {
			t.Fatalf("seq assignment not monotonic across reconnect: %v", persisted)
		}
	}
	if len(persisted) != 3 {
		t.Fatalf("expected 3 seq assignments (hello/buffered event/hello), got %v", persisted)
	}
	mu.Unlock()

	seen := map[int64]bool{}
	for _, f := range s.frames() {
		if seen[f.Seq] {
			t.Fatalf("duplicate seq on wire: %v", s.frames())
		}
		seen[f.Seq] = true
	}
}

func TestCommandDispatchAndTokenRotation(t *testing.T) {
	s := &fakeServer{t: t, welcome: protocol.Welcome{
		NodeID: "n1", Credential: "cred", SessionToken: "sess", HeartbeatIntervalS: 3600,
	}}
	srv := startServer(t, s)

	gotCmd := make(chan protocol.Frame, 1)
	gotToken := make(chan string, 2)
	hooks := testHooks(s)
	hooks.OnCommand = func(f protocol.Frame) { gotCmd <- f }
	hooks.OnSessionToken = func(tok string) { gotToken <- tok }

	s.afterWelcome = func(ctx context.Context, ws *websocket.Conn) {
		time.Sleep(50 * time.Millisecond)
		raw := []byte(`{"type":"job.stop","protocol_version":1,"command_id":"cmd-9","payload":{"job_id":"j1","session_token":"sess2"}}`)
		ws.Write(ctx, websocket.MessageText, raw)
	}

	c := New(Config{ServerURL: wsURL(srv)}, hooks, 7)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	waitFor(t, func() bool { return len(s.frames()) > 0 }, "hello")
	if s.frames()[0].Seq != 8 {
		t.Fatalf("hello seq = %d, want 8 (last_seq=7 +1)", s.frames()[0].Seq)
	}

	if tok := <-gotToken; tok != "sess" {
		t.Fatalf("initial token = %q", tok)
	}

	select {
	case f := <-gotCmd:
		if f.Type != protocol.TypeJobStop || f.CommandID != "cmd-9" {
			t.Fatalf("command frame mismatch: %+v", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no command dispatched")
	}
	if tok := <-gotToken; tok != "sess2" {
		t.Fatalf("rotated token = %q", tok)
	}
}
