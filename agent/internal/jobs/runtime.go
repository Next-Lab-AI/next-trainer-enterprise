package jobs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type State string

const (
	StateCreated    State = "CREATED"
	StateWaiting    State = "WAITING"
	StateRunning    State = "RUNNING"
	StateFinished   State = "FINISHED"
	StateFailed     State = "FAILED"
	StateTerminated State = "TERMINATED"
)

type runtimeFact struct {
	JobID         string    `json:"job_id"`
	PID           int       `json:"pid,omitempty"`
	State         State     `json:"state"`
	Engine        string    `json:"engine"`
	EngineVersion string    `json:"engine_version"`
	GPUs          []int     `json:"gpus,omitempty"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func runtimePath(root, jobID string) string {
	return filepath.Join(root, "jobs", jobID, "runtime.json")
}

func writeRuntimeFact(root string, f runtimeFact) error {
	f.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(runtimePath(root, f.JobID)), 0o755); err != nil {
		return err
	}
	tmp := runtimePath(root, f.JobID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, runtimePath(root, f.JobID))
}

func readRuntimeFact(root, jobID string) (runtimeFact, error) {
	var f runtimeFact
	data, err := os.ReadFile(runtimePath(root, jobID))
	if err != nil {
		return f, err
	}
	return f, json.Unmarshal(data, &f)
}

func listRuntimeFacts(root string) ([]runtimeFact, error) {
	entries, err := os.ReadDir(filepath.Join(root, "jobs"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []runtimeFact
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if f, err := readRuntimeFact(root, e.Name()); err == nil {
			out = append(out, f)
		}
	}
	return out, nil
}
