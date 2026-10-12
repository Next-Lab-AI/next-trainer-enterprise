package jobs

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

type logSink struct {
	m *Manager

	mu    sync.Mutex
	lines []json.RawMessage
}

func newLogSink(m *Manager) *logSink {
	return &logSink{m: m}
}

func (s *logSink) Enqueue(lines []json.RawMessage) {
	s.mu.Lock()
	s.lines = append(s.lines, lines...)
	full := len(s.lines) >= 50
	s.mu.Unlock()
	if full {
		s.Flush()
	}
}

func (s *logSink) Flush() {
	s.mu.Lock()
	if len(s.lines) == 0 {
		s.mu.Unlock()
		return
	}
	batch := s.lines
	s.lines = nil
	s.mu.Unlock()
	if err := s.m.sender.Send(context.Background(), protocol.TypeLog, protocol.Log{Lines: batch}); err != nil {
		s.m.agentLog.Warn("log batch send failed: " + err.Error())
	}
}

func (s *logSink) flushLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Flush()
			return
		case <-t.C:
			s.Flush()
		}
	}
}
