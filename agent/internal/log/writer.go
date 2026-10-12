package log

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

type Sink interface {
	Enqueue(lines []json.RawMessage)
}

type Logger struct {
	mu       sync.Mutex
	f        *os.File
	nodeID   string
	jobID    string
	source   string
	redactor *Redactor
	sink     Sink
}

func NewAgentLogger(root, nodeID string, sink Sink, redactor *Redactor) (*Logger, error) {
	return open(filepath.Join(root, "logs", "agent.jsonl"), nodeID, "", "agent", sink, redactor)
}

func NewJobLogger(root, nodeID, jobID, source string, sink Sink, redactor *Redactor) (*Logger, error) {
	return open(filepath.Join(root, "jobs", jobID, "stdout.jsonl"), nodeID, jobID, source, sink, redactor)
}

func open(path, nodeID, jobID, source string, sink Sink, redactor *Redactor) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("log: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("log: open %s: %w", path, err)
	}
	return &Logger{f: f, nodeID: nodeID, jobID: jobID, source: source, sink: sink, redactor: redactor}, nil
}

func (l *Logger) Log(level, msg string, extra map[string]any) {
	if l.redactor != nil {
		msg = l.redactor.Redact(msg)
	}
	line := protocol.LogLine{
		Ts:     time.Now().UTC().Format(time.RFC3339),
		Level:  level,
		Source: l.source,
		NodeID: l.nodeID,
		JobID:  l.jobID,
		Msg:    msg,
		Extra:  extra,
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return
	}
	l.mu.Lock()
	l.f.Write(append(raw, '\n'))
	l.mu.Unlock()
	if l.sink != nil {
		l.sink.Enqueue([]json.RawMessage{raw})
	}
}

func (l *Logger) Info(msg string)  { l.Log("info", msg, nil) }
func (l *Logger) Warn(msg string)  { l.Log("warn", msg, nil) }
func (l *Logger) Error(msg string) { l.Log("error", msg, nil) }

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
