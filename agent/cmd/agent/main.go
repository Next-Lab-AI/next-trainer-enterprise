package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"

	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/config"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/conn"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/fetch"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/gpu"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/jobs"
	agentlog "github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/log"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/provision"
)

var agentVersion = "dev"

func main() {
	cfg, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		slog.Error("flags", "err", err)
		os.Exit(2)
	}
	slog.Info("next-trainer-agent", "version", agentVersion)
	if err := os.MkdirAll(cfg.Root, 0o700); err != nil {
		slog.Error("root", "err", err)
		os.Exit(1)
	}

	identity, err := config.LoadIdentity(cfg.Root)
	if err != nil {
		identity = config.Identity{}
	}

	redact := agentlog.NewRedactor(cfg.Root, agentlog.SensitiveValuesFromEnv(os.Environ()))
	agentLog, err := agentlog.NewAgentLogger(cfg.Root, identity.NodeID, nil, redact)
	if err != nil {
		slog.Error("agent log", "err", err)
		os.Exit(1)
	}
	defer agentLog.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	g := &gpu.NVML{}
	fetcher := fetch.New(cfg.Root, fetch.WsToHTTPBase(cfg.Server), func() string { return identity.SessionToken }, fetch.S3ConfigFromEnv)

	baseEnvs := config.ProbeBaseEnvs(ctx, cfg.BasePythons)
	buf := jobs.NewRingBuffer(cfg.BufferMB << 20)

	var mgr *jobs.Manager
	c := conn.New(conn.Config{
		ServerURL: cfg.Server,
		Buffer:    buf,
	}, conn.Hooks{
		Hello: func() protocol.Hello {
			topo, _ := g.Topology(ctx)
			hostname, _ := os.Hostname()
			return protocol.Hello{
				NodeID:       identity.NodeID,
				Credential:   identity.Credential,
				AccessToken:  cfg.Token,
				AgentVersion: agentVersion,
				Hostname:     hostname,
				Description:  cfg.Description,
				Pool:         cfg.Pool,
				GPUs:         gpu.ToProtocol(topo),
				CPU:          runtime.GOARCH,
				MemMB:        gpu.HostMemTotalMB(),
				BaseEnvs:     baseEnvs,
			}
		},
		HeartbeatFacts: func() protocol.Heartbeat {
			return mgr.HeartbeatFacts(context.Background())
		},
		OnWelcome: func(w protocol.Welcome) {
			identity.NodeID = w.NodeID
			if w.Credential != "" {
				identity.Credential = w.Credential
			}
			identity.SessionToken = w.SessionToken
			if err := config.SaveIdentity(cfg.Root, identity); err != nil {
				agentLog.Error("save identity: " + err.Error())
			}
			agentLog.Info("registered as " + w.NodeID)
		},
		OnCommand: func(f protocol.Frame) {
			mgr.HandleCommand(context.Background(), f)
		},
		OnSessionToken: func(tok string) {
			identity.SessionToken = tok
			if err := config.SaveIdentity(cfg.Root, identity); err != nil {
				agentLog.Error("save identity: " + err.Error())
			}
		},
		PersistSeq: func(seq int64) {
			identity.LastSeq = seq
			config.SaveIdentity(cfg.Root, identity)
		},
	}, identity.LastSeq)

	prov := provision.New(cfg.Root, gpuVendor(ctx, g), fetcher, func() (provision.LineSink, error) { return agentLog, nil }, baseEnvs)
	mgr = jobs.NewManager(cfg.Root, identity.NodeID, senderAdapter{c}, g, fetcher, prov, agentLog, redact, buf, cfg.AllowPaths...)
	mgr.Start(ctx, 10*time.Second)
	mgr.Reconcile(ctx)

	agentLog.Info("agent starting")
	if err := c.Run(ctx); err != nil && ctx.Err() == nil {
		agentLog.Error("conn run: " + err.Error())
		os.Exit(1)
	}
}

type senderAdapter struct{ c *conn.Conn }

func (s senderAdapter) Send(ctx context.Context, msgType string, payload any) error {
	return s.c.Send(ctx, msgType, payload)
}

func gpuVendor(ctx context.Context, g gpu.Interface) string {
	topo, err := g.Topology(ctx)
	if err != nil || len(topo) == 0 {
		return "nvidia"
	}
	return topo[0].Vendor
}
