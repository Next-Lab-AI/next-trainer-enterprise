package jobs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"

	agentexec "github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/exec"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/fetch"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/gpu"
	agentlog "github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/log"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/provision"
)

type Sender interface {
	Send(ctx context.Context, msgType string, payload any) error
}

type Job struct {
	ID        string
	State     State
	Start     protocol.JobStart
	proc      processHandle
	startedAt time.Time
	logger    *agentlog.Logger
	lastError string
	artifacts []artifactRecord
	mu        sync.Mutex
}

type processHandle interface {
	PID() int
	Stop(grace time.Duration) error
	Kill() error
	Wait() error
}

type artifactRecord struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type Manager struct {
	root     string
	nodeID   string
	sender   Sender
	gpu      gpu.Interface
	fetcher  *fetch.Fetcher
	prov     *provision.Provisioner
	redact   *agentlog.Redactor
	agentLog *agentlog.Logger
	Buffer   *RingBuffer

	mu       sync.Mutex
	jobs     map[string]*Job
	seenCmds map[string]bool

	maintSem chan struct{}
	logSink  *logSink
}

func NewManager(root, nodeID string, sender Sender, g gpu.Interface, f *fetch.Fetcher, p *provision.Provisioner, agentLog *agentlog.Logger, redact *agentlog.Redactor, bufferMB int) *Manager {
	m := &Manager{
		root:     root,
		nodeID:   nodeID,
		sender:   sender,
		gpu:      g,
		fetcher:  f,
		prov:     p,
		redact:   redact,
		agentLog: agentLog,
		Buffer:   NewRingBuffer(bufferMB << 20),
		jobs:     map[string]*Job{},
		seenCmds: map[string]bool{},
		maintSem: make(chan struct{}, 2),
	}
	m.logSink = newLogSink(m)
	return m
}

func (m *Manager) Start(ctx context.Context, metricsInterval time.Duration) {
	go m.logSink.flushLoop(ctx)
	if metricsInterval > 0 {
		go m.metricsLoop(ctx, metricsInterval)
	}
}

func (m *Manager) metricsLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			mx, err := m.Metrics(ctx)
			if err != nil {
				continue
			}
			m.sender.Send(ctx, protocol.TypeMetrics, mx)
		}
	}
}

func (m *Manager) HandleCommand(ctx context.Context, f protocol.Frame) {
	if f.CommandID != "" {
		m.mu.Lock()
		if m.seenCmds[f.CommandID] {
			m.mu.Unlock()
			return
		}
		m.seenCmds[f.CommandID] = true
		m.mu.Unlock()
	}

	switch f.Type {
	case protocol.TypeJobStart:
		cmd, err := protocol.DecodePayload[protocol.JobStart](f)
		if err != nil {
			m.agentLog.Error("bad job.start: " + err.Error())
			return
		}
		m.startJob(ctx, cmd)
	case protocol.TypeJobStop:
		cmd, err := protocol.DecodePayload[protocol.JobStop](f)
		if err != nil {
			return
		}
		m.stopJob(cmd.JobID, cmd.GraceS, cmd.Preempt)
	case protocol.TypeJobKill:
		cmd, err := protocol.DecodePayload[protocol.JobKill](f)
		if err != nil {
			return
		}
		m.killJob(cmd.JobID)
	case protocol.TypeProvision:
		cmd, err := protocol.DecodePayload[protocol.Provision](f)
		if err != nil {
			return
		}
		go m.runProvision(ctx, cmd)
	case protocol.TypeEnvGC:
		cmd, err := protocol.DecodePayload[protocol.EnvGC](f)
		if err != nil {
			return
		}
		go m.runEnvGC(ctx, cmd)
	}
}

func (m *Manager) emit(job *Job, eventType string, data map[string]any) {
	ev := protocol.Event{EventType: eventType, Data: data}
	if job != nil {
		ev.JobID = job.ID
		if !job.startedAt.IsZero() {
			ev.ElapsedS = int64(time.Since(job.startedAt).Seconds())
		}
	}
	if err := m.sender.Send(context.Background(), protocol.TypeEvent, ev); err != nil {
		m.agentLog.Warn("event send failed: " + err.Error())
	}
}

func (m *Manager) runProvision(ctx context.Context, cmd protocol.Provision) {
	m.maintSem <- struct{}{}
	defer func() { <-m.maintSem }()

	ready, _, err := m.prov.Provision(ctx, cmd)
	data := map[string]any{"engine": cmd.Engine, "version": cmd.Version}
	if err == nil && ready {
		m.emit(nil, protocol.EventProvisionReady, data)
		return
	}
	if err != nil {
		data["error"] = err.Error()
	}
	m.emit(nil, protocol.EventProvisionBroken, data)
}

func (m *Manager) runEnvGC(ctx context.Context, cmd protocol.EnvGC) {
	m.maintSem <- struct{}{}
	defer func() { <-m.maintSem }()

	removed, err := m.fetcher.GC(ctx, cmd.OlderThanDays)
	if err != nil {
		m.agentLog.Error("env.gc: " + err.Error())
	}
	removed = append(removed, m.gcJobOutputs(cmd.OlderThanDays)...)
	m.emit(nil, protocol.EventEnvGCDone, map[string]any{"removed": removed})
}

func (m *Manager) gcJobOutputs(olderThanDays int) []string {
	if olderThanDays <= 0 {
		olderThanDays = 30
	}
	cutoff := time.Now().Add(-time.Duration(olderThanDays) * 24 * time.Hour)
	facts, err := listRuntimeFacts(m.root)
	if err != nil {
		return nil
	}
	var removed []string
	for _, f := range facts {
		if f.State == StateRunning || f.State == StateWaiting || f.State == StateCreated {
			continue
		}
		if f.UpdatedAt.After(cutoff) {
			continue
		}
		out := filepath.Join(m.root, "jobs", f.JobID, "output")
		if _, err := os.Stat(out); err == nil {
			if err := os.RemoveAll(out); err == nil {
				removed = append(removed, "jobs/"+f.JobID+"/output")
			}
		}
	}
	return removed
}

func (m *Manager) HeartbeatFacts(ctx context.Context) protocol.Heartbeat {
	m.mu.Lock()
	var jobs []protocol.JobStatusFact
	var busy map[int]bool = map[int]bool{}
	for _, j := range m.jobs {
		if j.State == StateRunning || j.State == StateWaiting {
			jobs = append(jobs, protocol.JobStatusFact{JobID: j.ID, Status: string(j.State)})
			for _, g := range j.Start.GPUs {
				busy[g] = true
			}
		}
	}
	m.mu.Unlock()

	var free []int
	if topo, err := m.gpu.Topology(ctx); err == nil {
		for _, g := range topo {
			if !busy[g.Index] {
				free = append(free, g.Index)
			}
		}
	}
	return protocol.Heartbeat{Jobs: jobs, FreeGPUs: free}
}

func (m *Manager) Metrics(ctx context.Context) (protocol.Metrics, error) {
	samples, err := m.gpu.Sample(ctx)
	if err != nil {
		return protocol.Metrics{}, err
	}
	return protocol.Metrics{Samples: gpu.ToSamples(samples, 0)}, nil
}

func (m *Manager) Reconcile(ctx context.Context) {
	facts, err := listRuntimeFacts(m.root)
	if err != nil {
		return
	}
	for _, f := range facts {
		switch f.State {
		case StateRunning, StateWaiting, StateCreated:
			if f.PID > 0 && agentexec.ProcessAlive(f.PID) {
				agentexec.KillTree(f.PID)
			}
			f.State = StateFailed
			f.PID = 0
			writeRuntimeFact(m.root, f)
			m.emit(&Job{ID: f.JobID}, protocol.EventJobFailed, map[string]any{
				"code": "agent_restarted",
				"msg":  "agent restarted while job was active",
			})
		}
	}
}

func (m *Manager) jobDir(jobID string) string {
	return filepath.Join(m.root, "jobs", jobID)
}
