package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/registry"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type Handler interface {
	OnEvent(nodeID string, seq int64, e protocol.Event)
	OnMetrics(nodeID string, seq int64, m protocol.Metrics)
	OnLog(nodeID string, seq int64, l protocol.Log)
	OnAssetReady(nodeID string, seq int64, a protocol.AssetReady)
	OnAssetFailed(nodeID string, seq int64, a protocol.AssetFailed)
	OnFile(nodeID string, seq int64, f protocol.FileChunk)
	OnHeartbeat(nodeID string, hb protocol.Heartbeat)
	OnNodeOnline(nodeID string)
	OnNodeOffline(nodeID string)
}

type Config struct {
	HeartbeatIntervalS int
	Logger             *slog.Logger
}

type conn struct {
	ws           *websocket.Conn
	nodeID       string
	sessionToken string
	lastSeen     atomic.Int64
}

type Hub struct {
	reg *registry.Registry
	st  store.Store
	cfg Config
	now func() time.Time

	mu       sync.Mutex
	conns    map[string]*conn
	tokens   map[string]string
	seen     map[string]map[int64]struct{}
	baseEnvs map[string][]protocol.BaseEnv
	hd       Handler
}

func NewHub(reg *registry.Registry, st store.Store, cfg Config) *Hub {
	if cfg.HeartbeatIntervalS <= 0 {
		cfg.HeartbeatIntervalS = 15
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Hub{
		reg:      reg,
		st:       st,
		cfg:      cfg,
		now:      time.Now,
		conns:    make(map[string]*conn),
		tokens:   make(map[string]string),
		seen:     make(map[string]map[int64]struct{}),
		baseEnvs: make(map[string][]protocol.BaseEnv),
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	_, raw, err := c.Read(ctx)
	cancel()
	if err != nil {
		c.Close(websocket.StatusProtocolError, "handshake")
		return
	}
	f, err := protocol.Decode(raw)
	if err != nil {
		reason := protocol.CloseBadToken
		if errors.Is(err, protocol.ErrVersionIncompatible) {
			reason = protocol.CloseVersionIncompatible
		}
		c.Close(websocket.StatusPolicyViolation, reason)
		return
	}
	if f.Type != protocol.TypeHello {
		c.Close(websocket.StatusPolicyViolation, protocol.CloseBadToken)
		return
	}
	hello, err := protocol.DecodePayload[protocol.Hello](f)
	if err != nil {
		c.Close(websocket.StatusPolicyViolation, protocol.CloseBadToken)
		return
	}
	res, err := h.reg.HandleHello(r.Context(), hello)
	if err != nil {
		c.Close(websocket.StatusPolicyViolation, closeReason(err))
		return
	}
	h.serveConn(r.Context(), c, res, hello)
}

func closeReason(err error) string {
	switch {
	case errors.Is(err, registry.ErrRevoked):
		return protocol.CloseRevoked
	case errors.Is(err, registry.ErrWindowClosed):
		return protocol.CloseWindowClosed
	default:
		return protocol.CloseBadToken
	}
}

func (h *Hub) serveConn(ctx context.Context, c *websocket.Conn, res *registry.HelloResult, hello protocol.Hello) {
	nodeID := res.Node.NodeID
	sessionToken := auth.NewToken()
	cc := &conn{ws: c, nodeID: nodeID, sessionToken: sessionToken}
	cc.lastSeen.Store(h.now().Unix())

	h.mu.Lock()
	old, replaced := h.conns[nodeID]
	if replaced {
		delete(h.tokens, old.sessionToken)
	}
	h.conns[nodeID] = cc
	h.tokens[sessionToken] = nodeID
	h.baseEnvs[nodeID] = hello.BaseEnvs
	h.mu.Unlock()
	if replaced {
		go old.ws.Close(websocket.StatusPolicyViolation, "replaced")
	}

	welcome, err := protocol.NewFrame(protocol.TypeWelcome, "", 0, protocol.Welcome{
		NodeID:             nodeID,
		Credential:         res.Credential,
		SessionToken:       sessionToken,
		HeartbeatIntervalS: h.cfg.HeartbeatIntervalS,
	})
	if err != nil {
		c.Close(websocket.StatusInternalError, "welcome")
		return
	}
	data, err := protocol.Encode(welcome)
	if err != nil {
		c.Close(websocket.StatusInternalError, "welcome")
		return
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = c.Write(wctx, websocket.MessageText, data)
	cancel()
	if err != nil {
		c.Close(websocket.StatusInternalError, "welcome")
		return
	}

	if h.handler() != nil {
		h.handler().OnNodeOnline(nodeID)
	}
	h.cfg.Logger.Info("node connected", "node_id", nodeID, "hostname", res.Node.Hostname, "new", res.NewNode)
	defer func() {
		h.mu.Lock()
		if h.conns[nodeID] == cc {
			delete(h.conns, nodeID)
			delete(h.tokens, sessionToken)
			h.mu.Unlock()
			if err := h.reg.MarkOffline(context.Background(), nodeID); err != nil {
				h.cfg.Logger.Warn("mark offline", "err", err)
			}
			if h.handler() != nil {
				h.handler().OnNodeOffline(nodeID)
			}
		} else {
			h.mu.Unlock()
		}
		c.Close(websocket.StatusNormalClosure, "session end")
		h.cfg.Logger.Info("node disconnected", "node_id", nodeID)
	}()

	for {
		_, raw, err := c.Read(ctx)
		if err != nil {
			return
		}
		cc.lastSeen.Store(h.now().Unix())
		f, err := protocol.Decode(raw)
		if err != nil {
			h.cfg.Logger.Warn("bad frame", "node_id", nodeID, "err", err)
			continue
		}
		if h.dup(nodeID, f.Seq) {
			continue
		}
		h.route(nodeID, f)
	}
}

func (h *Hub) dup(nodeID string, seq int64) bool {
	if seq == 0 {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.seen[nodeID]
	if !ok {
		set = make(map[int64]struct{})
		h.seen[nodeID] = set
	}
	if _, dup := set[seq]; dup {
		return true
	}
	set[seq] = struct{}{}
	return false
}

func (h *Hub) route(nodeID string, f protocol.Frame) {
	hd := h.handler()
	switch f.Type {
	case protocol.TypeHeartbeat:
		hb, err := protocol.DecodePayload[protocol.Heartbeat](f)
		if err != nil {
			return
		}
		if err := h.reg.Touch(context.Background(), nodeID); err != nil {
			h.cfg.Logger.Warn("touch", "err", err)
		}
		if hd != nil {
			hd.OnHeartbeat(nodeID, hb)
		}
	case protocol.TypeEvent:
		e, err := protocol.DecodePayload[protocol.Event](f)
		if err == nil && hd != nil {
			hd.OnEvent(nodeID, f.Seq, e)
		}
	case protocol.TypeMetrics:
		m, err := protocol.DecodePayload[protocol.Metrics](f)
		if err == nil && hd != nil {
			hd.OnMetrics(nodeID, f.Seq, m)
		}
	case protocol.TypeLog:
		l, err := protocol.DecodePayload[protocol.Log](f)
		if err == nil && hd != nil {
			hd.OnLog(nodeID, f.Seq, l)
		}
	case protocol.TypeAssetReady:
		a, err := protocol.DecodePayload[protocol.AssetReady](f)
		if err == nil && hd != nil {
			hd.OnAssetReady(nodeID, f.Seq, a)
		}
	case protocol.TypeAssetFailed:
		a, err := protocol.DecodePayload[protocol.AssetFailed](f)
		if err == nil && hd != nil {
			hd.OnAssetFailed(nodeID, f.Seq, a)
		}
	case protocol.TypeFile:
		fc, err := protocol.DecodePayload[protocol.FileChunk](f)
		if err == nil && hd != nil {
			hd.OnFile(nodeID, f.Seq, fc)
		}
	default:
		h.cfg.Logger.Warn("unknown frame type", "node_id", nodeID, "type", f.Type)
	}
}

func (h *Hub) handler() Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hd
}

func (h *Hub) SetHandler(hd Handler) {
	h.mu.Lock()
	h.hd = hd
	h.mu.Unlock()
}

func (h *Hub) SendCommand(ctx context.Context, nodeID, msgType string, payload any, issuedBy string) (string, error) {
	h.mu.Lock()
	cc, ok := h.conns[nodeID]
	h.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("ws: node %s not connected", nodeID)
	}
	commandID := store.NewID()
	f, err := protocol.NewFrame(msgType, commandID, 0, payload)
	if err != nil {
		return "", err
	}
	data, err := protocol.Encode(f)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(payload)
	cmd := &store.Command{
		CommandID: commandID, NodeID: nodeID, Type: msgType,
		Payload: raw, IssuedBy: issuedBy, IssuedAt: h.now().Unix(),
	}
	if err := h.st.Jobs().CreateCommand(ctx, cmd); err != nil {
		return "", err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cc.ws.Write(wctx, websocket.MessageText, data); err != nil {
		return "", err
	}
	return commandID, nil
}

func (h *Hub) Online(nodeID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[nodeID]
	return ok
}

func (h *Hub) BaseEnvs(nodeID string) []protocol.BaseEnv {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.baseEnvs[nodeID]
}

func (h *Hub) AuthenticateSession(token string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	nodeID, ok := h.tokens[token]
	return nodeID, ok
}

func (h *Hub) RunJanitor(ctx context.Context) {
	interval := time.Duration(h.cfg.HeartbeatIntervalS) * time.Second
	timeout := 2 * interval
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		h.mu.Lock()
		var stale []*conn
		for _, cc := range h.conns {
			if h.now().Unix()-cc.lastSeen.Load() > int64(timeout/time.Second) {
				stale = append(stale, cc)
			}
		}
		h.mu.Unlock()
		for _, cc := range stale {
			h.cfg.Logger.Warn("heartbeat timeout", "node_id", cc.nodeID)
			cc.ws.Close(websocket.StatusPolicyViolation, "heartbeat timeout")
		}
	}
}
