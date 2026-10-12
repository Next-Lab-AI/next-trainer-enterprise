package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"

	agentexec "github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/exec"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/fetch"
)

type LineSink interface {
	Log(level, msg string, extra map[string]any)
}

type EnvMeta struct {
	Engine      string    `json:"engine"`
	Version     string    `json:"version"`
	Fingerprint string    `json:"fingerprint"`
	EnvMode     string    `json:"env_mode"`
	BasePython  string    `json:"base_python,omitempty"`
	AuditOK     bool      `json:"audit_ok"`
	AuditedAt   time.Time `json:"audited_at"`
}

type Provisioner struct {
	root         string
	gpuVendor    string
	fetcher      *fetch.Fetcher
	newLogger    func() (LineSink, error)
	baseEnvPaths []string
	baseTorch    map[string]string
}

func New(root, gpuVendor string, fetcher *fetch.Fetcher, newLogger func() (LineSink, error), baseEnvs []protocol.BaseEnv) *Provisioner {
	p := &Provisioner{root: root, gpuVendor: gpuVendor, fetcher: fetcher, newLogger: newLogger}
	p.baseTorch = map[string]string{}
	for _, be := range baseEnvs {
		p.baseEnvPaths = append(p.baseEnvPaths, be.Python)
		p.baseTorch[be.Python] = be.Torch
	}
	return p
}

func (p *Provisioner) EngineDir(engine, version string) string {
	return filepath.Join(p.root, "engines", engine, version)
}

func (p *Provisioner) envDir(engine, fp string) string {
	return filepath.Join(p.root, "envs", engine+"-"+fp)
}

func (p *Provisioner) fingerprint(engineDir string, m Manifest, manifestRaw []byte, envMode, basePython string) (string, error) {
	spec, err := os.ReadFile(filepath.Join(engineDir, m.EnvSpec))
	if err != nil {
		return "", fmt.Errorf("provision: read env_spec %s: %w", m.EnvSpec, err)
	}
	h := sha256.New()
	h.Write(manifestRaw)
	h.Write(spec)
	h.Write([]byte(envMode))
	if envMode == "external" {
		h.Write([]byte(basePython))
		h.Write([]byte(p.baseTorch[basePython]))
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

func (p *Provisioner) hookPath(engineDir, hook string) (string, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	path := filepath.Join(engineDir, "engine.d", runtime.GOOS, p.gpuVendor, hook+ext)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("provision: engine does not support %s/%s (missing %s)", runtime.GOOS, p.gpuVendor, path)
	}
	return path, nil
}

type auditResult struct {
	OK     bool           `json:"ok"`
	Facts  map[string]any `json:"facts"`
	Errors []string       `json:"errors"`
}

type hookFrame struct {
	Type  string          `json:"type"`
	Level string          `json:"level"`
	Msg   string          `json:"msg"`
	Stage string          `json:"stage"`
	Pct   int             `json:"pct"`
	Audit json.RawMessage `json:"-"`
}

func (p *Provisioner) runHook(ctx context.Context, engineDir, hook string, env map[string]string, logger LineSink) (auditResult, error) {
	hookPath, err := p.hookPath(engineDir, hook)
	if err != nil {
		return auditResult{}, err
	}
	var audit auditResult
	var sawAudit bool
	proc, err := agentexec.Start(ctx, agentexec.Options{
		Name: hookPath,
		Dir:  engineDir,
		Env:  agentexec.BuildEnv(os.Environ(), env),
	}, func(stream, line string) {
		var f hookFrame
		if err := json.Unmarshal([]byte(line), &f); err == nil && f.Type != "" {
			switch f.Type {
			case "audit":
				json.Unmarshal([]byte(line), &audit)
				sawAudit = true
			case "log":
				logger.Log(f.Level, f.Msg, nil)
			case "progress":
				logger.Log("info", fmt.Sprintf("%s %d%%", f.Stage, f.Pct), map[string]any{"stage": f.Stage, "pct": f.Pct})
			case "error":
				logger.Log("error", f.Msg, nil)
			default:
				logger.Log("info", line, nil)
			}
			return
		}
		logger.Log("info", line, map[string]any{"stream": stream})
	})
	if err != nil {
		return auditResult{}, err
	}
	waitErr := proc.Wait()
	if hook == "audit" && !sawAudit {
		return auditResult{}, fmt.Errorf("provision: audit hook did not emit audit frame")
	}
	if waitErr != nil {
		return audit, fmt.Errorf("provision: hook %s failed: %w", hook, waitErr)
	}
	return audit, nil
}

func (p *Provisioner) buildHookEnv(engineDir, venvPath string, m Manifest, envMode, basePython, jobDir string, sources *protocol.DownloadSources) map[string]string {
	env := map[string]string{
		"ENGINE_ROOT":    engineDir,
		"VENV_PATH":      venvPath,
		"PYTHON_VERSION": m.PythonVersion,
		"ENV_MODE":       envMode,
		"UV_PATH":        p.uvPath(),
	}
	if basePython != "" {
		env["BASE_PYTHON"] = basePython
	}
	if jobDir != "" {
		env["JOB_DIR"] = jobDir
	}
	if sources != nil {
		if raw, err := json.Marshal(sources); err == nil {
			env["DOWNLOAD_SOURCES"] = string(raw)
		}
	}
	if v := os.Getenv("CUDA_EXTRA"); v != "" {
		env["CUDA_EXTRA"] = v
	}
	return env
}

func (p *Provisioner) uvPath() string {
	embedded := filepath.Join(p.root, "bin", "uv")
	if _, err := os.Stat(embedded); err == nil {
		return embedded
	}
	return "uv"
}

func (p *Provisioner) Provision(ctx context.Context, cmd protocol.Provision) (ready bool, envDir string, err error) {
	logger, lerr := p.newLogger()
	if lerr != nil {
		return false, "", lerr
	}

	engineDir := p.EngineDir(cmd.Engine, cmd.Version)
	if _, err := os.Stat(filepath.Join(engineDir, "engine.toml")); err != nil {
		if cmd.Package.URI == "" {
			return false, "", fmt.Errorf("provision: engine source missing at %s and no package asset", engineDir)
		}
		logger.Log("info", "fetching engine package", map[string]any{"uri": cmd.Package.URI})
		if err := p.fetchEnginePackage(ctx, cmd.Package, engineDir); err != nil {
			return false, "", err
		}
	}
	m, manifestRaw, err := LoadManifest(engineDir)
	if err != nil {
		return false, "", err
	}

	envMode := cmd.EnvMode
	if envMode == "" {
		envMode = "venv"
	}
	if envMode == "external" {
		ok := false
		for _, bp := range p.baseEnvPaths {
			if bp == cmd.BasePython {
				ok = true
				break
			}
		}
		if !ok {
			return false, "", fmt.Errorf("provision: base_python %q not in registered base_envs", cmd.BasePython)
		}
	}

	fp, err := p.fingerprint(engineDir, m, manifestRaw, envMode, cmd.BasePython)
	if err != nil {
		return false, "", err
	}
	envDir = p.envDir(cmd.Engine, fp)

	if meta, err := readMeta(envDir); err == nil && meta.AuditOK {
		logger.Log("info", "env already provisioned (idempotent)", map[string]any{"fingerprint": fp})
		return true, envDir, nil
	}

	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return false, "", err
	}
	hookEnv := p.buildHookEnv(engineDir, envDir, m, envMode, cmd.BasePython, "", cmd.DownloadSources)

	if _, err := p.runHook(ctx, engineDir, "install", hookEnv, logger); err != nil {
		writeMeta(envDir, EnvMeta{Engine: cmd.Engine, Version: cmd.Version, Fingerprint: fp, EnvMode: envMode, BasePython: cmd.BasePython})
		return false, envDir, err
	}
	audit, err := p.runHook(ctx, engineDir, "audit", hookEnv, logger)
	meta := EnvMeta{
		Engine: cmd.Engine, Version: cmd.Version, Fingerprint: fp,
		EnvMode: envMode, BasePython: cmd.BasePython,
		AuditOK: err == nil && audit.OK, AuditedAt: time.Now().UTC(),
	}
	writeMeta(envDir, meta)
	if err != nil {
		return false, envDir, err
	}
	if !audit.OK {
		return false, envDir, fmt.Errorf("provision: audit not ok: %s", strings.Join(audit.Errors, "; "))
	}
	return true, envDir, nil
}

func (p *Provisioner) fetchEnginePackage(ctx context.Context, pkg protocol.Asset, engineDir string) error {
	cachePath, _, err := p.fetcher.Fetch(ctx, pkg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		return err
	}
	return extractTarGz(cachePath, engineDir)
}

func (p *Provisioner) Preflight(ctx context.Context, engineDir, envDir, jobDir string, logger LineSink) error {
	m, _, err := LoadManifest(engineDir)
	if err != nil {
		return err
	}
	meta, err := readMeta(envDir)
	if err != nil {
		return fmt.Errorf("provision: env meta: %w", err)
	}
	env := p.buildHookEnv(engineDir, envDir, m, meta.EnvMode, meta.BasePython, jobDir, nil)
	_, err = p.runHook(ctx, engineDir, "preflight", env, logger)
	return err
}

func (p *Provisioner) FindReadyEnv(engine, version string) (string, bool) {
	entries, err := os.ReadDir(filepath.Join(p.root, "envs"))
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), engine+"-") {
			continue
		}
		dir := filepath.Join(p.root, "envs", e.Name())
		meta, err := readMeta(dir)
		if err == nil && meta.AuditOK && meta.Engine == engine && meta.Version == version {
			return dir, true
		}
	}
	return "", false
}

// ScanEngines 扫描 root/engines/<name>/<version>/，上报引擎清单与能力。
// 目录可能是软链（dev 联调直接链到 vendor），用 os.Stat 判目录。
func (p *Provisioner) ScanEngines() []protocol.EngineInfo {
	isDir := func(path string) bool {
		st, err := os.Stat(path)
		return err == nil && st.IsDir()
	}
	root := filepath.Join(p.root, "engines")
	names, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []protocol.EngineInfo
	for _, n := range names {
		nameDir := filepath.Join(root, n.Name())
		if !isDir(nameDir) {
			continue
		}
		versions, err := os.ReadDir(nameDir)
		if err != nil {
			continue
		}
		for _, v := range versions {
			dir := filepath.Join(nameDir, v.Name())
			if !isDir(dir) {
				continue
			}
			m, _, err := LoadManifest(dir)
			if err != nil {
				continue
			}
			_, ready := p.FindReadyEnv(n.Name(), v.Name())
			out = append(out, protocol.EngineInfo{
				Name:         n.Name(),
				Version:      v.Name(),
				Capabilities: m.Capabilities,
				Ready:        ready,
			})
		}
	}
	return out
}

func (p *Provisioner) ScanEnvs() []EnvMeta {
	var out []EnvMeta
	entries, err := os.ReadDir(filepath.Join(p.root, "envs"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if meta, err := readMeta(filepath.Join(p.root, "envs", e.Name())); err == nil {
			out = append(out, meta)
		}
	}
	return out
}

func readMeta(envDir string) (EnvMeta, error) {
	data, err := os.ReadFile(filepath.Join(envDir, ".provision-meta.json"))
	if err != nil {
		return EnvMeta{}, err
	}
	var m EnvMeta
	return m, json.Unmarshal(data, &m)
}

func writeMeta(envDir string, m EnvMeta) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(envDir, ".provision-meta.json"), data, 0o644)
}
