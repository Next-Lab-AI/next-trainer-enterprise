package provision

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"

	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/fetch"
)

type memSink struct {
	mu   sync.Mutex
	logs []string
}

func (m *memSink) Log(level, msg string, extra map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, level+": "+msg)
}

func newMemSink(m *memSink) func() (LineSink, error) {
	return func() (LineSink, error) { return m, nil }
}

func writeFakeEngine(t *testing.T, root, engine, version string, auditOK bool) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("hook scripts are linux sh")
	}
	dir := filepath.Join(root, "engines", engine, version)
	hookDir := filepath.Join(dir, "engine.d", "linux", "nvidia")
	os.MkdirAll(hookDir, 0o755)

	os.WriteFile(filepath.Join(dir, "engine.toml"), []byte(`
name = "fake"
version = "0.1"
entry = "train.py"
accelerator = "none"
min_gpus = 1
max_gpus = 1
env_spec = "requirements.txt"
kind = "train"
`), 0o644)
	os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("torch==2.5.1\n"), 0o644)

	auditLine := `{"type":"audit","ok":true,"facts":{"torch":"2.5.1"}}`
	if !auditOK {
		auditLine = `{"type":"audit","ok":false,"errors":["cuda missing"]}`
	}
	hooks := map[string]string{
		"install": `#!/bin/sh
echo '{"type":"progress","stage":"venv","pct":50}'
echo '{"type":"log","level":"info","msg":"installing"}'
mkdir -p "$VENV_PATH"
`,
		"audit": `#!/bin/sh
echo ` + "'" + auditLine + `'
`,
		"preflight": `#!/bin/sh
test -n "$JOB_DIR" || exit 1
echo '{"type":"log","level":"info","msg":"preflight ok"}'
`,
	}
	for name, body := range hooks {
		p := filepath.Join(hookDir, name+".sh")
		os.WriteFile(p, []byte(body), 0o755)
	}
	return dir
}

func testProvisioner(root string, sink *memSink) *Provisioner {
	f := fetch.New(root, "", func() string { return "" }, fetch.S3ConfigFromEnv)
	return New(root, "nvidia", f, newMemSink(sink), []protocol.BaseEnv{
		{Python: "/usr/bin/python3", Torch: "2.5.1", CUDA: "12.8"},
	})
}

func TestProvisionLifecycle(t *testing.T) {
	root := t.TempDir()
	writeFakeEngine(t, root, "fake", "0.1", true)
	sink := &memSink{}
	p := testProvisioner(root, sink)

	ready, envDir, err := p.Provision(context.Background(), protocol.Provision{Engine: "fake", Version: "0.1"})
	if err != nil || !ready {
		t.Fatalf("provision: %v ready=%v", err, ready)
	}
	meta, err := readMeta(envDir)
	if err != nil || !meta.AuditOK || meta.EnvMode != "venv" {
		t.Fatalf("meta: %+v %v", meta, err)
	}

	ready2, envDir2, err := p.Provision(context.Background(), protocol.Provision{Engine: "fake", Version: "0.1"})
	if err != nil || !ready2 || envDir2 != envDir {
		t.Fatalf("idempotent provision: %v %v %v", ready2, envDir2, err)
	}

	foundDir, ok := p.FindReadyEnv("fake", "0.1")
	if !ok || foundDir != envDir {
		t.Fatalf("FindReadyEnv: %v %v", foundDir, ok)
	}
	if len(p.ScanEnvs()) != 1 {
		t.Fatalf("ScanEnvs: %v", p.ScanEnvs())
	}

	jobDir := filepath.Join(root, "jobs", "j1")
	os.MkdirAll(jobDir, 0o755)
	if err := p.Preflight(context.Background(), p.EngineDir("fake", "0.1"), envDir, jobDir, sink); err != nil {
		t.Fatalf("preflight: %v", err)
	}
}

func TestProvisionAuditBroken(t *testing.T) {
	root := t.TempDir()
	writeFakeEngine(t, root, "bad", "0.1", false)
	p := testProvisioner(root, &memSink{})

	ready, envDir, err := p.Provision(context.Background(), protocol.Provision{Engine: "bad", Version: "0.1"})
	if ready || err == nil {
		t.Fatalf("want broken, got ready=%v err=%v", ready, err)
	}
	meta, _ := readMeta(envDir)
	if meta.AuditOK {
		t.Fatal("broken env marked ok")
	}
}

func TestProvisionExternalValidation(t *testing.T) {
	root := t.TempDir()
	writeFakeEngine(t, root, "fake", "0.1", true)
	p := testProvisioner(root, &memSink{})

	_, _, err := p.Provision(context.Background(), protocol.Provision{
		Engine: "fake", Version: "0.1", EnvMode: "external", BasePython: "/usr/bin/python3",
	})
	if err != nil {
		t.Fatalf("registered base python rejected: %v", err)
	}

	_, _, err = p.Provision(context.Background(), protocol.Provision{
		Engine: "fake", Version: "0.1", EnvMode: "external", BasePython: "/opt/sneaky/bin/python",
	})
	if err == nil {
		t.Fatal("unregistered base python accepted")
	}
}

func TestFingerprintModes(t *testing.T) {
	root := t.TempDir()
	dir := writeFakeEngine(t, root, "fake", "0.1", true)
	p := testProvisioner(root, &memSink{})
	m, raw, _ := LoadManifest(dir)

	fp1, _ := p.fingerprint(dir, m, raw, "venv", "")
	fp2, _ := p.fingerprint(dir, m, raw, "external", "/usr/bin/python3")
	if fp1 == fp2 {
		t.Fatal("venv and external fingerprints collide")
	}
	fp3, _ := p.fingerprint(dir, m, raw, "venv", "")
	if fp1 != fp3 {
		t.Fatal("fingerprint not stable")
	}
}
