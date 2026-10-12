package log

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

type sliceSink struct {
	mu    sync.Mutex
	lines []json.RawMessage
}

func (s *sliceSink) Enqueue(lines []json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, lines...)
}

func (s *sliceSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lines)
}

func TestJobLoggerWritesJSONL(t *testing.T) {
	root := t.TempDir()
	sink := &sliceSink{}
	l, err := NewJobLogger(root, "n1", "j1", "engine", sink, NewRedactor(root, nil))
	if err != nil {
		t.Fatalf("NewJobLogger: %v", err)
	}
	defer l.Close()

	l.Info("hello")
	l.Log("info", "step done", map[string]any{"step": 120})

	data, err := os.ReadFile(filepath.Join(root, "jobs", "j1", "stdout.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	var line protocol.LogLine
	if err := json.Unmarshal([]byte(lines[1]), &line); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if line.Source != "engine" || line.NodeID != "n1" || line.JobID != "j1" || line.Msg != "step done" {
		t.Fatalf("line mismatch: %+v", line)
	}
	if line.Extra["step"] != float64(120) {
		t.Fatalf("extra mismatch: %+v", line.Extra)
	}
	if _, err := time.Parse(time.RFC3339, line.Ts); err != nil {
		t.Fatalf("ts not RFC3339: %q", line.Ts)
	}
	if sink.count() != 2 {
		t.Fatalf("sink count = %d", sink.count())
	}
}

func TestRedaction(t *testing.T) {
	root := "/opt/agent"
	r := NewRedactor(root, []string{"supersecretvalue123", "short"})
	cases := map[string]string{
		"token=supersecretvalue123 ok":     "token=*** ok",
		"AWS_SECRET_KEY=abcdefgh12345678":  "AWS_SECRET_KEY=***",
		`{"hf_token": "hf_abcdefgh12345"}`: `{"hf_token": "***"}`,
		"/opt/agent/jobs/j1/output":        "jobs/j1/output",
		"nothing sensitive here":           "nothing sensitive here",
		"value short kept":                 "value short kept",
	}
	for in, want := range cases {
		if got := r.Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSensitiveValuesFromEnv(t *testing.T) {
	vals := SensitiveValuesFromEnv([]string{
		"AWS_SECRET_ACCESS_KEY=abcdefgh12345678",
		"HF_TOKEN=hf_123456789",
		"PATH=/usr/bin",
		"EMPTY_KEY=",
	})
	if len(vals) != 2 {
		t.Fatalf("values = %v", vals)
	}
}
