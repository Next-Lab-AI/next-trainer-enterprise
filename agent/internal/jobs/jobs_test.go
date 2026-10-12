package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"

	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/fetch"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/gpu"
	agentlog "github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/log"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/provision"
)

type memSender struct {
	mu     sync.Mutex
	frames []sentFrame
}

type sentFrame struct {
	msgType string
	payload any
}

func (s *memSender) Send(ctx context.Context, msgType string, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, sentFrame{msgType, payload})
	return nil
}

func (s *memSender) events() []protocol.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []protocol.Event
	for _, f := range s.frames {
		if f.msgType == protocol.TypeEvent {
			out = append(out, f.payload.(protocol.Event))
		}
	}
	return out
}

func (s *memSender) waitEvent(t *testing.T, jobID, eventType string) protocol.Event {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range s.events() {
			if ev.JobID == jobID && ev.EventType == eventType {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting event %s for job %s", eventType, jobID)
	return protocol.Event{}
}

func setupManager(t *testing.T, root string, sender *memSender) *Manager {
	t.Helper()
	engineDir := filepath.Join(root, "engines", "fake", "0.1")
	hookDir := filepath.Join(engineDir, "engine.d", "linux", "nvidia")
	os.MkdirAll(hookDir, 0o755)
	os.WriteFile(filepath.Join(engineDir, "engine.toml"), []byte(`
name = "fake"
version = "0.1"
entry = "train.py"
accelerator = "none"
min_gpus = 1
max_gpus = 8
env_spec = "requirements.txt"
kind = "train"
`), 0o644)
	os.WriteFile(filepath.Join(engineDir, "requirements.txt"), []byte("x==1\n"), 0o644)
	os.WriteFile(filepath.Join(engineDir, "train.py"), []byte(`
import json, os, sys
print(json.dumps({"type":"log","level":"info","msg":"starting"}))
print(json.dumps({"type":"progress","step":1,"total":2,"loss":0.5}))
print(json.dumps({"type":"artifact","kind":"checkpoint","path":"output/ckpt.bin"}))
open(os.path.join("output","ckpt.bin"),"w").write("w")
print(json.dumps({"type":"progress","step":2,"total":2}))
`), 0o644)
	slowTrain := `
import json, sys, time
print(json.dumps({"type":"log","level":"info","msg":"starting slow"}))
sys.stdout.flush()
time.sleep(300)
`
	os.WriteFile(filepath.Join(engineDir, "slow_train.py"), []byte(slowTrain), 0o644)

	hooks := map[string]string{
		"install":   "#!/bin/sh\nmkdir -p \"$VENV_PATH\"\n",
		"audit":     "#!/bin/sh\necho '{\"type\":\"audit\",\"ok\":true}'\n",
		"preflight": "#!/bin/sh\necho '{\"type\":\"log\",\"level\":\"info\",\"msg\":\"ok\"}'\n",
	}
	for name, body := range hooks {
		os.WriteFile(filepath.Join(hookDir, name+".sh"), []byte(body), 0o755)
	}

	agentLog, err := agentlog.NewAgentLogger(root, "n1", nil, agentlog.NewRedactor(root, nil))
	if err != nil {
		t.Fatalf("agent log: %v", err)
	}
	t.Cleanup(func() { agentLog.Close() })

	f := fetch.New(root, "", func() string { return "" }, fetch.S3ConfigFromEnv)
	p := provision.New(root, "nvidia", f, func() (provision.LineSink, error) { return agentLog, nil }, nil)
	g := &gpu.Stub{GPUs: []gpu.GPU{{Index: 0, Model: "GB10", Vendor: "nvidia"}}}

	m := NewManager(root, "n1", sender, g, f, p, agentLog, agentlog.NewRedactor(root, nil), NewRingBuffer(8<<20))

	if _, _, err := p.Provision(context.Background(), protocol.Provision{Engine: "fake", Version: "0.1"}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	return m
}

func frame(msgType, commandID string, payload any) protocol.Frame {
	f, err := protocol.NewFrame(msgType, commandID, 0, payload)
	if err != nil {
		panic(err)
	}
	return f
}

func TestJobLifecycleHappyPath(t *testing.T) {
	root := t.TempDir()
	sender := &memSender{}
	m := setupManager(t, root, sender)

	src := filepath.Join(t.TempDir(), "ds.bin")
	os.WriteFile(src, []byte("data"), 0o644)

	m.HandleCommand(context.Background(), frame(protocol.TypeJobStart, "cmd-1", protocol.JobStart{
		JobID: "j1", Engine: "fake", EngineVersion: "0.1",
		ConfigTOML: `output_dir = "output"`,
		Assets:     []protocol.Asset{{URI: "file://" + src, Dest: "ds.bin"}},
		GPUs:       []int{0}, Lane: protocol.LaneCompute,
	}))

	wait := sender.waitEvent(t, "j1", protocol.EventJobWaiting)
	if wait.Data["reason"] != "assets" {
		t.Fatalf("waiting event: %+v", wait.Data)
	}
	fin := sender.waitEvent(t, "j1", protocol.EventJobFinished)
	if fin.Data["exit_code"] != 0 {
		t.Fatalf("finished: %+v", fin.Data)
	}
	if _, ok := fin.Data["artifacts"]; !ok {
		t.Fatalf("artifacts missing in finished: %+v", fin.Data)
	}
	if _, err := os.Stat(filepath.Join(root, "jobs", "j1", "ds.bin")); err != nil {
		t.Fatalf("asset not linked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "jobs", "j1", "output", "ckpt.bin")); err != nil {
		t.Fatalf("artifact not produced: %v", err)
	}

	progressSeen := false
	for _, ev := range sender.events() {
		if ev.JobID == "j1" && ev.EventType == protocol.EventJobProgress {
			progressSeen = true
		}
	}
	if !progressSeen {
		t.Fatal("no progress events")
	}
}

func TestJobAssetFailed(t *testing.T) {
	root := t.TempDir()
	sender := &memSender{}
	m := setupManager(t, root, sender)

	m.HandleCommand(context.Background(), frame(protocol.TypeJobStart, "cmd-2", protocol.JobStart{
		JobID: "j2", Engine: "fake", EngineVersion: "0.1",
		ConfigTOML: `x = 1`,
		Assets:     []protocol.Asset{{URI: "file:///nonexistent/x.bin", SHA256: "deadbeef"}},
		GPUs:       []int{0}, Lane: protocol.LaneCompute,
	}))
	ev := sender.waitEvent(t, "j2", protocol.EventJobFailed)
	if ev.Data["code"] != "asset_failed" {
		t.Fatalf("failed event: %+v", ev.Data)
	}
}

func TestJobStopAndPreempt(t *testing.T) {
	root := t.TempDir()
	sender := &memSender{}
	m := setupManager(t, root, sender)

	slowDir := filepath.Join(root, "engines", "fake", "0.1")
	os.WriteFile(filepath.Join(slowDir, "engine.toml"), []byte(`
name = "fake"
version = "0.1"
entry = "slow_train.py"
accelerator = "none"
min_gpus = 1
max_gpus = 8
env_spec = "requirements.txt"
kind = "train"
`), 0o644)

	m.HandleCommand(context.Background(), frame(protocol.TypeJobStart, "cmd-3", protocol.JobStart{
		JobID: "j3", Engine: "fake", EngineVersion: "0.1",
		ConfigTOML: `x = 1`, GPUs: []int{0}, Lane: protocol.LaneCompute,
	}))
	sender.waitEvent(t, "j3", protocol.EventJobStarting)

	m.HandleCommand(context.Background(), frame(protocol.TypeJobStop, "cmd-4", protocol.JobStop{JobID: "j3", Preempt: true, GraceS: 1}))
	ev := sender.waitEvent(t, "j3", protocol.EventJobTerminated)
	if ev.Data["reason"] != "preempted" {
		t.Fatalf("terminated event: %+v", ev.Data)
	}
}

func TestCommandIdDedup(t *testing.T) {
	root := t.TempDir()
	sender := &memSender{}
	m := setupManager(t, root, sender)

	f := frame(protocol.TypeEnvGC, "cmd-gc", protocol.EnvGC{OlderThanDays: 0})
	m.HandleCommand(context.Background(), f)
	m.HandleCommand(context.Background(), f)
	time.Sleep(300 * time.Millisecond)

	count := 0
	for _, ev := range sender.events() {
		if ev.EventType == protocol.EventEnvGCDone {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("env.gc.done count = %d", count)
	}
}

func TestRingBufferGap(t *testing.T) {
	b := NewRingBuffer(512)
	big := strings.Repeat("x", 300)
	for i := 1; i <= 5; i++ {
		f, _ := protocol.NewFrame(protocol.TypeLog, "", int64(i), protocol.Log{Lines: nil})
		f.Payload = append(f.Payload[:0], big...)
		b.Append(f)
	}
	if b.Len() >= 5 {
		t.Fatalf("no eviction, len=%d", b.Len())
	}
	from, to, ok := b.TakeGap()
	if !ok || from < 1 || to <= from {
		t.Fatalf("gap = %d..%d ok=%v", from, to, ok)
	}
	_, _, ok = b.TakeGap()
	if ok {
		t.Fatal("gap not cleared")
	}
	frames := b.Replay()
	if b.Len() != 0 || len(frames) == 0 {
		t.Fatalf("replay: len=%d drained=%d", b.Len(), len(frames))
	}
}

func TestHeartbeatFacts(t *testing.T) {
	root := t.TempDir()
	sender := &memSender{}
	m := setupManager(t, root, sender)

	hb := m.HeartbeatFacts(context.Background())
	if len(hb.FreeGPUs) != 1 || hb.FreeGPUs[0] != 0 {
		t.Fatalf("free gpus: %+v", hb.FreeGPUs)
	}
}

func TestReconcile(t *testing.T) {
	root := t.TempDir()
	sender := &memSender{}
	m := setupManager(t, root, sender)

	writeRuntimeFact(root, runtimeFact{
		JobID: "ghost", PID: 999999, State: StateRunning,
		Engine: "fake", EngineVersion: "0.1",
	})
	m.Reconcile(context.Background())
	ev := sender.waitEvent(t, "ghost", protocol.EventJobFailed)
	if ev.Data["code"] != "agent_restarted" {
		t.Fatalf("reconcile event: %+v", ev.Data)
	}
	f, _ := readRuntimeFact(root, "ghost")
	if f.State != StateFailed {
		t.Fatalf("ghost state = %s", f.State)
	}
}
