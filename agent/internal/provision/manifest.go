package provision

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Manifest struct {
	Name          string   `toml:"name"`
	Version       string   `toml:"version"`
	Entry         string   `toml:"entry"`
	Accelerator   string   `toml:"accelerator"`
	MinGPUs       int      `toml:"min_gpus"`
	MaxGPUs       int      `toml:"max_gpus"`
	EnvSpec       string   `toml:"env_spec"`
	PythonVersion string   `toml:"python_version"`
	Capabilities  []string `toml:"capabilities"`
	Kind          string   `toml:"kind"`
}

func LoadManifest(engineDir string) (Manifest, []byte, error) {
	path := filepath.Join(engineDir, "engine.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("provision: read engine.toml: %w", err)
	}
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return Manifest{}, nil, fmt.Errorf("provision: parse engine.toml: %w", err)
	}
	if m.Name == "" || m.EnvSpec == "" {
		return Manifest{}, nil, fmt.Errorf("provision: engine.toml missing name/env_spec")
	}
	return m, data, nil
}
