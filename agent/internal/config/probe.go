package config

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

const probeTimeout = 15 * time.Second

const probeScript = `
import json, sys
out = {"python": sys.executable}
try:
    import torch
    out["torch"] = torch.__version__
    out["cuda"] = torch.version.cuda or ""
except Exception:
    pass
print(json.dumps(out))
`

func ProbeBaseEnv(ctx context.Context, python string) protocol.BaseEnv {
	env := protocol.BaseEnv{Python: python}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, python, "-c", probeScript).Output()
	if err != nil {
		return env
	}
	var res struct {
		Torch string `json:"torch"`
		CUDA  string `json:"cuda"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return env
	}
	env.Torch = res.Torch
	env.CUDA = res.CUDA
	return env
}

func ProbeBaseEnvs(ctx context.Context, declared []string) []protocol.BaseEnv {
	seen := map[string]bool{}
	var paths []string
	if p, err := exec.LookPath("python3"); err == nil {
		paths = append(paths, p)
		seen[p] = true
	}
	for _, p := range declared {
		if !seen[p] {
			paths = append(paths, p)
			seen[p] = true
		}
	}
	var envs []protocol.BaseEnv
	for _, p := range paths {
		envs = append(envs, ProbeBaseEnv(ctx, p))
	}
	return envs
}
