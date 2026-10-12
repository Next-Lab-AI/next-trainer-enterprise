package protocol

import "encoding/json"

type GPUInfo struct {
	Index      int    `json:"index"`
	UUID       string `json:"uuid"`
	Model      string `json:"model"`
	Vendor     string `json:"vendor"`
	MemTotalMB int64  `json:"mem_total_mb"`
}

type BaseEnv struct {
	Python string `json:"python"`
	Torch  string `json:"torch,omitempty"`
	CUDA   string `json:"cuda,omitempty"`
}

type Hello struct {
	NodeID       string            `json:"node_id,omitempty"`
	Credential   string            `json:"credential,omitempty"`
	AccessToken  string            `json:"access_token,omitempty"`
	AgentVersion string            `json:"agent_version"`
	Hostname     string            `json:"hostname"`
	Description  string            `json:"description,omitempty"`
	Pool         string            `json:"pool,omitempty"`
	GPUs         []GPUInfo         `json:"gpus"`
	CPU          string            `json:"cpu"`
	MemMB        int64             `json:"mem_mb"`
	Labels       map[string]string `json:"labels,omitempty"`
	BaseEnvs     []BaseEnv         `json:"base_envs,omitempty"`
}

type Welcome struct {
	NodeID             string         `json:"node_id"`
	Credential         string         `json:"credential,omitempty"`
	SessionToken       string         `json:"session_token"`
	HeartbeatIntervalS int            `json:"heartbeat_interval_s"`
	Config             map[string]any `json:"config,omitempty"`
}

type JobStatusFact struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type Heartbeat struct {
	Jobs     []JobStatusFact `json:"jobs"`
	FreeGPUs []int           `json:"free_gpus"`
	Engines  []EngineInfo    `json:"engines,omitempty"`
}

type EngineInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities,omitempty"`
	Ready        bool     `json:"ready"`
}

type Asset struct {
	URI    string `json:"uri"`
	Dest   string `json:"dest,omitempty"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

type DownloadSources struct {
	PipIndex     string `json:"pip_index,omitempty"`
	PytorchIndex string `json:"pytorch_index,omitempty"`
	HFEndpoint   string `json:"hf_endpoint,omitempty"`
	GitHubPrefix string `json:"github_prefix,omitempty"`
}

type Provision struct {
	Engine          string           `json:"engine"`
	Version         string           `json:"version"`
	Package         Asset            `json:"package"`
	Constraints     string           `json:"constraints,omitempty"`
	EnvMode         string           `json:"env_mode,omitempty"`
	BasePython      string           `json:"base_python,omitempty"`
	DownloadSources *DownloadSources `json:"download_sources,omitempty"`
}

type EnvGC struct {
	OlderThanDays int `json:"older_than_days,omitempty"`
}

type JobStart struct {
	JobID         string  `json:"job_id"`
	Engine        string  `json:"engine"`
	EngineVersion string  `json:"engine_version"`
	ConfigTOML    string  `json:"config_toml"`
	Assets        []Asset `json:"assets,omitempty"`
	GPUs          []int   `json:"gpus"`
	Lane          string  `json:"lane"`
}

type JobStop struct {
	JobID   string `json:"job_id"`
	Preempt bool   `json:"preempt,omitempty"`
	GraceS  int    `json:"grace_s,omitempty"`
}

type JobKill struct {
	JobID string `json:"job_id"`
}

type Event struct {
	EventType string         `json:"event_type"`
	JobID     string         `json:"job_id,omitempty"`
	ElapsedS  int64          `json:"elapsed_s,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type MetricSample struct {
	GPUIndex  int     `json:"gpu_index"`
	TsOffsetS int64   `json:"ts_offset_s"`
	Util      int     `json:"util"`
	MemUsedMB int64   `json:"mem_used_mb"`
	PowerW    float64 `json:"power_w,omitempty"`
	TempC     float64 `json:"temp_c,omitempty"`
}

type Metrics struct {
	Samples []MetricSample `json:"samples"`
}

type Log struct {
	Lines []json.RawMessage `json:"lines"`
}

type FileChunk struct {
	Path   string `json:"path"`
	Chunk  []byte `json:"chunk"`
	Offset int64  `json:"offset"`
	Last   bool   `json:"last"`
}

type AssetReady struct {
	URI          string `json:"uri"`
	JobID        string `json:"job_id,omitempty"`
	SHA256Actual string `json:"sha256_actual"`
}

type AssetFailed struct {
	URI   string `json:"uri"`
	JobID string `json:"job_id,omitempty"`
	Error string `json:"error"`
}

type LogLine struct {
	Ts     string         `json:"ts"`
	Level  string         `json:"level"`
	Source string         `json:"source"`
	NodeID string         `json:"node_id"`
	JobID  string         `json:"job_id,omitempty"`
	Msg    string         `json:"msg"`
	Extra  map[string]any `json:"extra,omitempty"`
}
