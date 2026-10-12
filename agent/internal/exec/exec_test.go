package exec

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(data)
	end := strings.LastIndex(s, ")")
	if end < 0 || end+2 >= len(s) {
		return true
	}
	fields := strings.Fields(s[end+2:])
	return len(fields) == 0 || fields[0] != "Z"
}

func TestLineScan(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	p, err := Start(context.Background(), Options{
		Name: "sh",
		Args: []string{"-c", "echo out1; echo err1 >&2; echo out2"},
	}, func(stream, line string) {
		mu.Lock()
		lines = append(lines, stream+":"+line)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, ",")
	for _, want := range []string{"stdout:out1", "stderr:err1", "stdout:out2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}

func TestStopSleepTree(t *testing.T) {
	p, err := Start(context.Background(), Options{
		Name: "sh",
		Args: []string{"-c", "sleep 300 & sleep 300 & wait"},
	}, func(string, string) {})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	root := p.PID()

	if err := p.Stop(50 * time.Millisecond); err != nil {
		t.Logf("Stop: %v", err)
	}
	if alive(root) {
		t.Fatalf("root %d still alive", root)
	}
}

func TestStopForkLoop(t *testing.T) {
	marker := strconv.Itoa(os.Getpid())
	p, err := Start(context.Background(), Options{
		Name: "sh",
		Args: []string{"-c", "for i in 1 2 3 4 5; do (sleep 60; echo " + marker + ") & done; sleep 60 & wait"},
	}, func(string, string) {})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	descBefore := snapshotDescendants(p.PID())
	if len(descBefore) < 2 {
		t.Fatalf("expected descendants, got %v", descBefore)
	}
	if err := p.Kill(); err != nil {
		t.Logf("Kill: %v", err)
	}
	for _, pid := range descBefore {
		if alive(pid) {
			t.Errorf("descendant %d survived tree kill", pid)
		}
	}
}

func TestStopIgnoresSIGTERM(t *testing.T) {
	p, err := Start(context.Background(), Options{
		Name: "sh",
		Args: []string{"-c", "trap '' TERM; while true; do sleep 1; done"},
	}, func(string, string) {})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	root := p.PID()

	start := time.Now()
	p.Stop(300 * time.Millisecond)
	elapsed := time.Since(start)
	if alive(root) {
		t.Fatalf("SIGTERM-ignoring process %d survived", root)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("stop took too long: %v", elapsed)
	}
}

func TestBuildEnv(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"AWS_SECRET_ACCESS_KEY=abcdefgh12345678",
		"HF_TOKEN=hf_123456789",
		"HOME=/home/x",
	}
	out := BuildEnv(base, map[string]string{"CUDA_VISIBLE_DEVICES": "0,1"})
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "abcdefgh12345678") || strings.Contains(joined, "hf_123456789") {
		t.Fatalf("sensitive vars leaked: %v", out)
	}
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/x", "CUDA_VISIBLE_DEVICES=0,1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in env", want)
		}
	}
}

func TestValidateConfigPaths(t *testing.T) {
	root := "/opt/agent"
	good := `
output_dir = "/opt/agent/jobs/j1/output"
dataset = "jobs/j1/data"
[model]
path = "datasets/abc/model.safetensors"
list = ["jobs/j1/a", "jobs/j1/b"]
`
	if err := ValidateConfigPaths([]byte(good), root); err != nil {
		t.Fatalf("good config rejected: %v", err)
	}

	bad := []string{
		`output_dir = "/etc/passwd"`,
		`output_dir = "/opt/agent/../../etc/shadow"`,
		`output_dir = "../../escape"`,
		`[x]
deep = [{ p = "/root/.ssh" }]`,
	}
	for i, b := range bad {
		if err := ValidateConfigPaths([]byte(b), root); err == nil {
			t.Errorf("bad config %d accepted", i)
		} else {
			fmt.Println(err)
		}
	}
}
