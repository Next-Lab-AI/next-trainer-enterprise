package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

func (s *Service) OnEvent(nodeID string, seq int64, e protocol.Event) {
	ctx := context.Background()
	payload, _ := json.Marshal(e.Data)
	ev := &store.Event{
		NodeID:   nodeID,
		JobID:    e.JobID,
		Type:     e.EventType,
		Payload:  payload,
		ServerTS: s.now().Unix(),
		Gap:      e.EventType == protocol.EventGap,
	}
	if seq > 0 {
		ev.AgentSeq = &seq
	}
	if e.ElapsedS > 0 {
		ev.ElapsedS = &e.ElapsedS
	}
	if err := s.st.Jobs().AppendEvents(ctx, []*store.Event{ev}); err != nil {
		s.cfg.Logger.Error("append event", "node_id", nodeID, "err", err)
		return
	}
	if ev.Seq == -1 {
		return
	}
	if e.JobID != "" {
		s.applyJobEvent(ctx, e, ev.ServerTS)
	}
}

func (s *Service) applyJobEvent(ctx context.Context, e protocol.Event, serverTS int64) {
	var status string
	var exitCode *int64
	var startedAt, endedAt *int64

	switch e.EventType {
	case protocol.EventJobCreated:
		status = store.JobStatusCreated
	case protocol.EventJobWaiting:
		status = store.JobStatusWaiting
	case protocol.EventJobStarting:
		status = store.JobStatusRunning
		startedAt = &serverTS
	case protocol.EventJobFinished:
		status = store.JobStatusFinished
		zero := int64(0)
		exitCode = &zero
		if v, ok := e.Data["exit_code"].(float64); ok {
			*exitCode = int64(v)
		}
		endedAt = &serverTS
	case protocol.EventJobFailed:
		status = store.JobStatusFailed
		endedAt = &serverTS
	case protocol.EventJobTerminated:
		status = store.JobStatusTerminated
		endedAt = &serverTS
	default:
		return
	}
	if err := s.st.Jobs().UpdateJobStatus(ctx, e.JobID, status, exitCode, startedAt, endedAt); err != nil {
		s.cfg.Logger.Warn("job status update", "job_id", e.JobID, "err", err)
		return
	}
	s.clearAlloc(e.JobID)
}

func (s *Service) clearAlloc(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.dispatched[jobID]
	if !ok {
		return
	}
	delete(s.dispatched, jobID)
	pend := s.pending[a.nodeID]
	keep := pend[:0]
	for _, g := range pend {
		still := false
		for _, ag := range a.gpus {
			if g == ag {
				still = true
				break
			}
		}
		if !still {
			keep = append(keep, g)
		}
	}
	s.pending[a.nodeID] = keep
}

func (s *Service) OnHeartbeat(nodeID string, hb protocol.Heartbeat) {
	s.mu.Lock()
	free := make([]int, len(hb.FreeGPUs))
	copy(free, hb.FreeGPUs)
	s.freeGPUs[nodeID] = free
	s.mu.Unlock()
}

func (s *Service) OnMetrics(nodeID string, seq int64, m protocol.Metrics) {
	ctx := context.Background()
	payload, _ := json.Marshal(m)
	ev := &store.Event{
		NodeID:   nodeID,
		Type:     "metrics",
		Payload:  payload,
		ServerTS: s.now().Unix(),
	}
	if seq > 0 {
		ev.AgentSeq = &seq
	}
	if err := s.st.Jobs().AppendEvents(ctx, []*store.Event{ev}); err != nil {
		s.cfg.Logger.Warn("append metrics", "err", err)
	}
}

func (s *Service) OnLog(nodeID string, seq int64, l protocol.Log) {
	byJob := make(map[string][]json.RawMessage)
	for _, line := range l.Lines {
		var probe protocol.LogLine
		if json.Unmarshal(line, &probe) != nil || probe.JobID == "" {
			continue
		}
		byJob[probe.JobID] = append(byJob[probe.JobID], line)
	}
	for jobID, lines := range byJob {
		f, err := s.logFile(jobID)
		if err != nil {
			s.cfg.Logger.Warn("log file", "err", err)
			continue
		}
		for _, line := range lines {
			f.Write(append(line, '\n'))
		}
		f.Close()
	}
}

func (s *Service) OnAssetReady(nodeID string, seq int64, a protocol.AssetReady) {
	s.appendSimpleEvent(nodeID, seq, "asset.ready", a.JobID, map[string]any{
		"uri": a.URI, "sha256_actual": a.SHA256Actual,
	})
}

func (s *Service) OnAssetFailed(nodeID string, seq int64, a protocol.AssetFailed) {
	s.appendSimpleEvent(nodeID, seq, "asset.failed", a.JobID, map[string]any{
		"uri": a.URI, "error": a.Error,
	})
}

func (s *Service) appendSimpleEvent(nodeID string, seq int64, typ, jobID string, data map[string]any) {
	ctx := context.Background()
	payload, _ := json.Marshal(data)
	ev := &store.Event{
		NodeID: nodeID, JobID: jobID, Type: typ, Payload: payload,
		ServerTS: s.now().Unix(),
	}
	if seq > 0 {
		ev.AgentSeq = &seq
	}
	if err := s.st.Jobs().AppendEvents(ctx, []*store.Event{ev}); err != nil {
		s.cfg.Logger.Warn("append event", "type", typ, "err", err)
	}
}

func (s *Service) OnFile(nodeID string, seq int64, fc protocol.FileChunk) {
	clean := filepath.Clean("/" + fc.Path)
	if strings.Contains(clean, "..") {
		return
	}
	dir := filepath.Join(s.cfg.LogDir, "files", nodeID, filepath.Dir(clean))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(s.cfg.LogDir, "files", nodeID, clean)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteAt(fc.Chunk, fc.Offset)
}

func (s *Service) OnNodeOnline(nodeID string) {}

func (s *Service) OnNodeOffline(nodeID string) {}
