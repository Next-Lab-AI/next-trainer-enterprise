package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"

	agentexec "github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/exec"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/fetch"
	agentlog "github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/log"
	"github.com/Next-Lab-AI/next-trainer-enterprise/agent/internal/provision"
)

func (m *Manager) startJob(ctx context.Context, cmd protocol.JobStart) {
	m.mu.Lock()
	if _, exists := m.jobs[cmd.JobID]; exists {
		m.mu.Unlock()
		m.emit(&Job{ID: cmd.JobID}, protocol.EventJobAlreadyRunning, nil)
		return
	}
	job := &Job{ID: cmd.JobID, State: StateCreated, Start: cmd}
	m.jobs[cmd.JobID] = job
	m.mu.Unlock()

	if f, err := readRuntimeFact(m.root, cmd.JobID); err == nil && f.PID > 0 && agentexec.ProcessAlive(f.PID) {
		m.mu.Lock()
		delete(m.jobs, cmd.JobID)
		m.mu.Unlock()
		m.emit(&Job{ID: cmd.JobID}, protocol.EventJobAlreadyRunning, map[string]any{"pid": f.PID})
		return
	}

	go m.runJob(ctx, job)
}

func (m *Manager) setState(job *Job, s State) {
	job.mu.Lock()
	job.State = s
	job.mu.Unlock()
	pid := 0
	if job.proc != nil {
		pid = job.proc.PID()
	}
	writeRuntimeFact(m.root, runtimeFact{
		JobID: job.ID, PID: pid, State: s,
		Engine: job.Start.Engine, EngineVersion: job.Start.EngineVersion,
		GPUs: job.Start.GPUs, StartedAt: job.startedAt,
	})
}

func (m *Manager) failJob(job *Job, code, msg string) {
	m.setState(job, StateFailed)
	m.emit(job, protocol.EventJobFailed, map[string]any{"code": code, "msg": msg})
}

func (m *Manager) runJob(ctx context.Context, job *Job) {
	jobDir := m.jobDir(job.ID)
	if err := os.MkdirAll(filepath.Join(jobDir, "output"), 0o755); err != nil {
		m.failJob(job, "io", err.Error())
		return
	}
	if err := agentexec.ValidateConfigPaths([]byte(job.Start.ConfigTOML), m.root); err != nil {
		m.failJob(job, "config_invalid", err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(jobDir, "config.toml"), []byte(job.Start.ConfigTOML), 0o644); err != nil {
		m.failJob(job, "io", err.Error())
		return
	}

	logger, err := agentlog.NewJobLogger(m.root, m.nodeID, job.ID, "engine", m.logSink, m.redact)
	if err != nil {
		m.failJob(job, "io", err.Error())
		return
	}
	job.logger = logger
	m.emit(job, protocol.EventJobCreated, nil)

	if !m.fetchAssets(ctx, job) {
		return
	}

	engineDir := m.prov.EngineDir(job.Start.Engine, job.Start.EngineVersion)
	envDir, ok := m.prov.FindReadyEnv(job.Start.Engine, job.Start.EngineVersion)
	if !ok {
		m.failJob(job, "env_not_ready", "no ready env for "+job.Start.Engine+" "+job.Start.EngineVersion+"; provision first")
		return
	}
	if err := m.prov.Preflight(ctx, engineDir, envDir, jobDir, job.logger); err != nil {
		m.failJob(job, "preflight", err.Error())
		return
	}

	m.launch(ctx, job, engineDir, envDir, jobDir)
}

func (m *Manager) fetchAssets(ctx context.Context, job *Job) bool {
	var missing []protocol.Asset
	for _, a := range job.Start.Assets {
		if _, ok := m.fetcher.Cached(a.URI, a.SHA256); !ok {
			missing = append(missing, a)
		}
	}
	if len(missing) == 0 {
		return m.linkAssets(job)
	}

	m.setState(job, StateWaiting)
	var uris []string
	for _, a := range missing {
		uris = append(uris, a.URI)
	}
	m.emit(job, protocol.EventJobWaiting, map[string]any{"reason": "assets", "missing": uris})

	for _, a := range job.Start.Assets {
		m.maintSem <- struct{}{}
		cachePath, sum, err := m.fetcher.Fetch(ctx, a)
		<-m.maintSem
		if err != nil {
			m.emit(job, protocol.EventJobFailed, map[string]any{"code": "asset_failed", "msg": err.Error()})
			m.sender.Send(context.Background(), protocol.TypeAssetFailed, protocol.AssetFailed{URI: a.URI, JobID: job.ID, Error: err.Error()})
			m.setState(job, StateFailed)
			return false
		}
		m.sender.Send(context.Background(), protocol.TypeAssetReady, protocol.AssetReady{URI: a.URI, JobID: job.ID, SHA256Actual: sum})
		_ = cachePath
	}
	return m.linkAssets(job)
}

func (m *Manager) linkAssets(job *Job) bool {
	for _, a := range job.Start.Assets {
		cachePath, _ := m.fetcher.Cached(a.URI, a.SHA256)
		if cachePath == "" {
			m.failJob(job, "asset_missing", a.URI)
			return false
		}
		if _, err := fetch.LinkIntoJob(cachePath, m.jobDir(job.ID), a.Dest); err != nil {
			m.failJob(job, "asset_link", err.Error())
			return false
		}
	}
	return true
}

func (m *Manager) launch(ctx context.Context, job *Job, engineDir, envDir, jobDir string) {
	job.startedAt = time.Now()
	m.setState(job, StateRunning)
	m.emit(job, protocol.EventJobStarting, map[string]any{"gpus": job.Start.GPUs})

	python := filepath.Join(envDir, "bin", "python")
	if _, err := os.Stat(python); err != nil {
		python = "python3"
	}
	entry := ""
	if man, _, err := provision.LoadManifest(engineDir); err == nil && man.Entry != "" {
		entry = filepath.Join(engineDir, man.Entry)
	}
	if entry == "" {
		m.failJob(job, "manifest", "engine entry unknown")
		return
	}

	var gpus []string
	for _, g := range job.Start.GPUs {
		gpus = append(gpus, strconv.Itoa(g))
	}
	env := agentexec.BuildEnv(os.Environ(), map[string]string{
		"CUDA_VISIBLE_DEVICES": strings.Join(gpus, ","),
		"JOB_DIR":              jobDir,
		"ENGINE_ROOT":          engineDir,
		"VENV_PATH":            envDir,
	})

	proc, err := agentexec.Start(ctx, agentexec.Options{
		Name: python,
		Args: []string{entry, "--config", filepath.Join(jobDir, "config.toml")},
		Dir:  jobDir,
		Env:  env,
	}, func(stream, line string) {
		m.handleEngineLine(job, stream, line)
	})
	if err != nil {
		m.failJob(job, "spawn", err.Error())
		return
	}
	job.proc = proc
	m.setState(job, StateRunning)

	waitErr := proc.Wait()
	m.afterExit(ctx, job, waitErr)
}

func (m *Manager) handleEngineLine(job *Job, stream, line string) {
	var frame struct {
		Type  string   `json:"type"`
		Level string   `json:"level"`
		Msg   string   `json:"msg"`
		Step  int      `json:"step"`
		Total int      `json:"total"`
		Loss  *float64 `json:"loss"`
		LR    *float64 `json:"lr"`
		Kind  string   `json:"kind"`
		Path  string   `json:"path"`
		Code  string   `json:"code"`
	}
	if stream == "stdout" && json.Unmarshal([]byte(line), &frame) == nil && frame.Type != "" {
		switch frame.Type {
		case "progress":
			data := map[string]any{"step": frame.Step, "total": frame.Total}
			if frame.Loss != nil {
				data["loss"] = *frame.Loss
			}
			if frame.LR != nil {
				data["lr"] = *frame.LR
			}
			m.emit(job, protocol.EventJobProgress, data)
		case "artifact":
			job.mu.Lock()
			job.artifacts = append(job.artifacts, artifactRecord{Kind: frame.Kind, Path: frame.Path})
			job.mu.Unlock()
			job.logger.Log("info", "artifact: "+frame.Path, map[string]any{"kind": frame.Kind})
		case "error":
			job.mu.Lock()
			job.lastError = frame.Msg
			job.mu.Unlock()
			job.logger.Log("error", frame.Msg, map[string]any{"code": frame.Code})
		case "log":
			job.logger.Log(frame.Level, frame.Msg, nil)
		default:
			job.logger.Log("info", line, nil)
		}
		return
	}
	job.logger.Log("info", line, map[string]any{"stream": stream})
}

func (m *Manager) afterExit(ctx context.Context, job *Job, waitErr error) {
	job.mu.Lock()
	lastErr := job.lastError
	artifacts := append([]artifactRecord(nil), job.artifacts...)
	terminated := job.State == StateTerminated
	job.mu.Unlock()

	if terminated {
		return
	}
	if waitErr == nil {
		m.uploadArtifacts(ctx, job, artifacts)
		m.setState(job, StateFinished)
		data := map[string]any{"exit_code": 0}
		if len(artifacts) > 0 {
			data["artifacts"] = artifacts
		}
		m.emit(job, protocol.EventJobFinished, data)
		return
	}
	msg := waitErr.Error()
	if lastErr != "" {
		msg = lastErr
	}
	m.failJob(job, "exit", msg)
}

func (m *Manager) uploadArtifacts(ctx context.Context, job *Job, artifacts []artifactRecord) {
	for _, a := range artifacts {
		local := filepath.Join(m.jobDir(job.ID), a.Path)
		if _, err := os.Stat(local); err != nil {
			continue
		}
		if err := m.fetcher.UploadArtifact(ctx, job.ID, local, a.Path); err != nil {
			job.logger.Log("warn", "artifact upload failed: "+err.Error(), map[string]any{"path": a.Path})
		}
	}
}

func (m *Manager) stopJob(jobID string, graceS int, preempt bool) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	m.mu.Unlock()
	if !ok || job.proc == nil {
		return
	}
	grace := time.Duration(graceS) * time.Second
	if graceS <= 0 {
		grace = 60 * time.Second
	}
	job.mu.Lock()
	job.State = StateTerminated
	job.mu.Unlock()
	job.proc.Stop(grace)
	reason := "stop"
	if preempt {
		reason = "preempted"
	}
	m.setState(job, StateTerminated)
	m.emit(job, protocol.EventJobTerminated, map[string]any{"reason": reason})
}

func (m *Manager) killJob(jobID string) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	m.mu.Unlock()
	if !ok || job.proc == nil {
		return
	}
	job.mu.Lock()
	job.State = StateTerminated
	job.mu.Unlock()
	job.proc.Kill()
	m.setState(job, StateTerminated)
	m.emit(job, protocol.EventJobTerminated, map[string]any{"reason": "kill"})
}
