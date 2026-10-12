package protocol

import (
	"errors"
	"strings"
	"testing"
)

func roundTrip[T any](t *testing.T, msgType string, in T) T {
	t.Helper()
	f, err := NewFrame(msgType, "cmd-1", 42, in)
	if err != nil {
		t.Fatalf("NewFrame: %v", err)
	}
	data, err := Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if back.Type != msgType || back.CommandID != "cmd-1" || back.Seq != 42 {
		t.Fatalf("envelope mismatch: %+v", back)
	}
	out, err := DecodePayload[T](back)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	return out
}

func TestRoundTripAllTypes(t *testing.T) {
	hello := roundTrip(t, TypeHello, Hello{
		Credential:   "cred",
		AgentVersion: "0.1.0",
		Hostname:     "spark-4ac3",
		Pool:         "vision",
		GPUs:         []GPUInfo{{Index: 0, UUID: "GPU-x", Model: "GB10", Vendor: "nvidia", MemTotalMB: 128000}},
		CPU:          "aarch64",
		MemMB:        122880,
		BaseEnvs:     []BaseEnv{{Python: "/usr/bin/python3", Torch: "2.5.1", CUDA: "12.8"}},
	})
	if hello.BaseEnvs[0].Torch != "2.5.1" || hello.GPUs[0].Vendor != "nvidia" {
		t.Fatalf("hello payload mismatch: %+v", hello)
	}

	welcome := roundTrip(t, TypeWelcome, Welcome{
		NodeID:             "node-1",
		Credential:         "cred",
		SessionToken:       "sess",
		HeartbeatIntervalS: 15,
		Config:             map[string]any{"buffer_mb": float64(8)},
	})
	if welcome.Config["buffer_mb"] != float64(8) {
		t.Fatalf("welcome config mismatch: %+v", welcome.Config)
	}

	hb := roundTrip(t, TypeHeartbeat, Heartbeat{
		Jobs:     []JobStatusFact{{JobID: "j1", Status: "RUNNING"}},
		FreeGPUs: []int{1, 2},
	})
	if len(hb.Jobs) != 1 || hb.FreeGPUs[1] != 2 {
		t.Fatalf("heartbeat mismatch: %+v", hb)
	}

	prov := roundTrip(t, TypeProvision, Provision{
		Engine:          "sd-scripts",
		Version:         "0.1.0",
		Package:         Asset{URI: "s3://bucket/pkg.tar.zst", SHA256: "abc"},
		EnvMode:         "external",
		BasePython:      "/usr/bin/python3",
		DownloadSources: &DownloadSources{PipIndex: "https://mirror/simple"},
	})
	if prov.EnvMode != "external" || prov.DownloadSources.PipIndex == "" {
		t.Fatalf("provision mismatch: %+v", prov)
	}

	gc := roundTrip(t, TypeEnvGC, EnvGC{OlderThanDays: 30})
	if gc.OlderThanDays != 30 {
		t.Fatalf("env.gc mismatch: %+v", gc)
	}

	js := roundTrip(t, TypeJobStart, JobStart{
		JobID:      "j1",
		Engine:     "sd-scripts",
		ConfigTOML: "x = 1",
		Assets:     []Asset{{URI: "file:///nas/ds.zip", Dest: "ds", SHA256: "def"}},
		GPUs:       []int{0, 1},
		Lane:       LaneCompute,
	})
	if js.Lane != LaneCompute || len(js.Assets) != 1 {
		t.Fatalf("job.start mismatch: %+v", js)
	}

	stop := roundTrip(t, TypeJobStop, JobStop{JobID: "j1", Preempt: true, GraceS: 60})
	if !stop.Preempt || stop.GraceS != 60 {
		t.Fatalf("job.stop mismatch: %+v", stop)
	}

	kill := roundTrip(t, TypeJobKill, JobKill{JobID: "j1"})
	if kill.JobID != "j1" {
		t.Fatalf("job.kill mismatch: %+v", kill)
	}

	ev := roundTrip(t, TypeEvent, Event{
		EventType: EventJobTerminated,
		JobID:     "j1",
		ElapsedS:  120,
		Data:      map[string]any{"reason": "preempted"},
	})
	if ev.Data["reason"] != "preempted" {
		t.Fatalf("event mismatch: %+v", ev)
	}

	m := roundTrip(t, TypeMetrics, Metrics{Samples: []MetricSample{
		{GPUIndex: 0, TsOffsetS: 5, Util: 98, MemUsedMB: 40000, PowerW: 120.5},
	}})
	if m.Samples[0].Util != 98 {
		t.Fatalf("metrics mismatch: %+v", m)
	}

	lg := roundTrip(t, TypeLog, Log{Lines: nil})
	if lg.Lines != nil {
		t.Fatalf("log mismatch: %+v", lg)
	}

	fc := roundTrip(t, TypeFile, FileChunk{Path: "jobs/j1/meta.json", Chunk: []byte("hello"), Offset: 0, Last: true})
	if string(fc.Chunk) != "hello" || !fc.Last {
		t.Fatalf("file chunk mismatch: %+v", fc)
	}

	ar := roundTrip(t, TypeAssetReady, AssetReady{URI: "file:///nas/ds.zip", JobID: "j1", SHA256Actual: "def"})
	if ar.SHA256Actual != "def" {
		t.Fatalf("asset.ready mismatch: %+v", ar)
	}

	af := roundTrip(t, TypeAssetFailed, AssetFailed{URI: "file:///nas/ds.zip", Error: "sha256 mismatch"})
	if af.Error == "" {
		t.Fatalf("asset.failed mismatch: %+v", af)
	}
}

func TestUnknownFieldsIgnored(t *testing.T) {
	raw := `{"type":"hello","protocol_version":1,"seq":1,"payload":{"agent_version":"0.1.0","hostname":"h","cpu":"x","mem_mb":1,"gpus":[],"future_field":{"a":1}}}`
	f, err := Decode([]byte(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	h, err := DecodePayload[Hello](f)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if h.Hostname != "h" {
		t.Fatalf("hello mismatch: %+v", h)
	}
}

func TestVersionIncompatible(t *testing.T) {
	raw := `{"type":"hello","protocol_version":99,"payload":{}}`
	_, err := Decode([]byte(raw))
	if !errors.Is(err, ErrVersionIncompatible) {
		t.Fatalf("want ErrVersionIncompatible, got %v", err)
	}

	f, err := NewFrame(TypeHello, "", 0, Hello{})
	if err != nil {
		t.Fatalf("NewFrame: %v", err)
	}
	f.ProtocolVersion = 2
	if _, err := Encode(f); !errors.Is(err, ErrVersionIncompatible) {
		t.Fatalf("Encode want ErrVersionIncompatible, got %v", err)
	}
}

func TestFileChunkBase64(t *testing.T) {
	f, err := NewFrame(TypeFile, "c", 1, FileChunk{Path: "p", Chunk: []byte{0x01, 0x02}, Last: true})
	if err != nil {
		t.Fatalf("NewFrame: %v", err)
	}
	data, err := Encode(f)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(data), `"chunk":"AQI="`) {
		t.Fatalf("chunk not base64: %s", data)
	}
}
