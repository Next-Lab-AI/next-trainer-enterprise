package config

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseFlags(t *testing.T) {
	c, err := ParseFlags([]string{
		"--server", "wss://ctrl", "--token", "tok", "--root", "/tmp/agent",
		"--description", "dev node", "--pool", "vision",
		"--base-python", "/usr/bin/python3", "--base-python", "/opt/py/bin/python",
	})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if c.Server != "wss://ctrl" || len(c.BasePythons) != 2 || c.BufferMB != 8 {
		t.Fatalf("config mismatch: %+v", c)
	}
}

func TestParseFlagsRequired(t *testing.T) {
	if _, err := ParseFlags([]string{"--root", "/tmp/agent"}); err == nil {
		t.Fatal("want error for missing --server")
	}
}

func TestIdentityRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadIdentity(root); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("want ErrNoIdentity, got %v", err)
	}
	id := Identity{NodeID: "n1", Credential: "c", SessionToken: "s", LastSeq: 42}
	if err := SaveIdentity(root, id); err != nil {
		t.Fatalf("SaveIdentity: %v", err)
	}
	back, err := LoadIdentity(root)
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if back != id {
		t.Fatalf("identity mismatch: %+v", back)
	}
	fi, err := os.Stat(IdentityPath(root))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("identity perm = %o, want 600", fi.Mode().Perm())
	}
	if entries, _ := filepath.Glob(filepath.Join(root, ".identity-*.tmp")); len(entries) != 0 {
		t.Fatalf("temp files left: %v", entries)
	}
}

func TestProbeBaseEnv(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 on PATH")
	}
	env := ProbeBaseEnv(context.Background(), python)
	if env.Python != python {
		t.Fatalf("env mismatch: %+v", env)
	}
	t.Logf("probed: %+v", env)
}
