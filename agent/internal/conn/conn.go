package conn

import (
	"context"
	"crypto/tls"
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
)

var errConnect = errors.New("conn: connect/handshake failure")

type SendBuffer interface {
	Append(f protocol.Frame)
	Replay() []protocol.Frame
}

type GapReporter interface {
	TakeGap() (from, to int64, ok bool)
}

type Hooks struct {
	Hello          func() protocol.Hello
	HeartbeatFacts func() protocol.Heartbeat
	OnWelcome      func(w protocol.Welcome)
	OnCommand      func(f protocol.Frame)
	OnSessionToken func(token string)
	PersistSeq     func(lastSeq int64)
}

type Config struct {
	ServerURL string
	TLSConfig *tls.Config
	Backoff   Backoff
	Buffer    SendBuffer
	Logger    *slog.Logger
}

type Backoff struct {
	Initial time.Duration
	Max     time.Duration
}

func (b Backoff) withDefaults() Backoff {
	if b.Initial <= 0 {
		b.Initial = time.Second
	}
	if b.Max <= 0 {
		b.Max = 60 * time.Second
	}
	return b
}

type Conn struct {
	cfg   Config
	hooks Hooks

	mu  sync.Mutex
	ws  *websocket.Conn
	seq atomic.Int64
}

func New(cfg Config, hooks Hooks, lastSeq int64) *Conn {
	c := &Conn{cfg: cfg, hooks: hooks}
	c.seq.Store(lastSeq)
	if c.cfg.Logger == nil {
		c.cfg.Logger = slog.Default()
	}
	return c
}

func (c *Conn) Send(ctx context.Context, msgType string, payload any) error {
	seq := c.seq.Add(1)
	if c.hooks.PersistSeq != nil {
		c.hooks.PersistSeq(seq)
	}
	f, err := protocol.NewFrame(msgType, "", seq, payload)
	if err != nil {
		return err
	}
	return c.sendFrame(ctx, f)
}

func (c *Conn) sendFrame(ctx context.Context, f protocol.Frame) error {
	c.mu.Lock()
	ws := c.ws
	c.mu.Unlock()
	if ws == nil {
		if c.cfg.Buffer != nil {
			c.cfg.Buffer.Append(f)
			return nil
		}
		return errors.New("conn: not connected")
	}
	data, err := protocol.Encode(f)
	if err != nil {
		return err
	}
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		if c.cfg.Buffer != nil {
			c.cfg.Buffer.Append(f)
			return nil
		}
		return err
	}
	return nil
}

func (c *Conn) Run(ctx context.Context) error {
	bo := c.cfg.Backoff.withDefaults()
	delay := bo.Initial
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !errors.Is(err, errConnect) {
			delay = bo.Initial
		}
		c.cfg.Logger.Warn("connection lost, retrying", "err", err, "in", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, bo.Max)
	}
}

func (c *Conn) session(ctx context.Context) error {
	opts := &websocket.DialOptions{}
	if c.cfg.TLSConfig != nil {
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: c.cfg.TLSConfig},
		}
	}
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	ws, _, err := websocket.Dial(dialCtx, c.cfg.ServerURL, opts)
	cancel()
	if err != nil {
		return fmt.Errorf("%w: dial: %v", errConnect, err)
	}
	fail := func(err error) error {
		ws.Close(websocket.StatusProtocolError, "handshake")
		return err
	}

	hello, err := protocol.NewFrame(protocol.TypeHello, "", c.seq.Add(1), c.hooks.Hello())
	if err != nil {
		return fail(fmt.Errorf("%w: hello build: %v", errConnect, err))
	}
	if c.hooks.PersistSeq != nil {
		c.hooks.PersistSeq(c.seq.Load())
	}
	data, err := protocol.Encode(hello)
	if err != nil {
		return fail(fmt.Errorf("%w: hello encode: %v", errConnect, err))
	}
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fail(fmt.Errorf("%w: hello write: %v", errConnect, err))
	}

	_, raw, err := ws.Read(ctx)
	if err != nil {
		reason := closeReason(err)
		ws.Close(websocket.StatusNormalClosure, "")
		return fmt.Errorf("%w: handshake rejected (%s)", errConnect, reason)
	}
	f, err := protocol.Decode(raw)
	if err != nil || f.Type != protocol.TypeWelcome {
		return fail(fmt.Errorf("%w: want welcome, got %q (%v)", errConnect, f.Type, err))
	}
	welcome, err := protocol.DecodePayload[protocol.Welcome](f)
	if err != nil {
		return fail(fmt.Errorf("%w: bad welcome: %v", errConnect, err))
	}

	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.ws = nil
		c.mu.Unlock()
		ws.Close(websocket.StatusNormalClosure, "session end")
	}()

	if c.hooks.OnSessionToken != nil && welcome.SessionToken != "" {
		c.hooks.OnSessionToken(welcome.SessionToken)
	}
	if c.hooks.OnWelcome != nil {
		c.hooks.OnWelcome(welcome)
	}
	c.replayBuffer(ctx)

	hbCtx, stopHB := context.WithCancel(ctx)
	defer stopHB()
	if interval := time.Duration(welcome.HeartbeatIntervalS) * time.Second; interval > 0 && c.hooks.HeartbeatFacts != nil {
		go c.heartbeatLoop(hbCtx, interval)
	}

	for {
		_, raw, err := ws.Read(ctx)
		if err != nil {
			return fmt.Errorf("conn: read: %w", err)
		}
		f, err := protocol.Decode(raw)
		if err != nil {
			if errors.Is(err, protocol.ErrVersionIncompatible) {
				return err
			}
			c.cfg.Logger.Warn("bad frame ignored", "err", err)
			continue
		}
		if tok := extractSessionToken(f.Payload); tok != "" && c.hooks.OnSessionToken != nil {
			c.hooks.OnSessionToken(tok)
		}
		if c.hooks.OnCommand != nil {
			c.hooks.OnCommand(f)
		}
	}
}

func (c *Conn) heartbeatLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.Send(ctx, protocol.TypeHeartbeat, c.hooks.HeartbeatFacts()); err != nil {
				c.cfg.Logger.Warn("heartbeat send failed", "err", err)
			}
		}
	}
}

func (c *Conn) replayBuffer(ctx context.Context) {
	if c.cfg.Buffer == nil {
		return
	}
	if gr, ok := c.cfg.Buffer.(GapReporter); ok {
		if from, to, has := gr.TakeGap(); has {
			c.Send(ctx, protocol.TypeEvent, protocol.Event{
				EventType: protocol.EventGap,
				Data:      map[string]any{"dropped_seq_from": from, "dropped_seq_to": to},
			})
		}
	}
	frames := c.cfg.Buffer.Replay()
	for i, f := range frames {
		c.mu.Lock()
		ws := c.ws
		c.mu.Unlock()
		if ws == nil {
			for _, rest := range frames[i:] {
				c.cfg.Buffer.Append(rest)
			}
			return
		}
		data, err := protocol.Encode(f)
		if err != nil {
			continue
		}
		if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
			for _, rest := range frames[i:] {
				c.cfg.Buffer.Append(rest)
			}
			return
		}
	}
}

func (c *Conn) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ws != nil
}

func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws != nil {
		c.ws.Close(websocket.StatusNormalClosure, "agent shutdown")
	}
}

func extractSessionToken(payload json.RawMessage) string {
	var probe struct {
		SessionToken string `json:"session_token"`
	}
	if json.Unmarshal(payload, &probe) != nil {
		return ""
	}
	return probe.SessionToken
}

func closeReason(err error) string {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return ""
}
