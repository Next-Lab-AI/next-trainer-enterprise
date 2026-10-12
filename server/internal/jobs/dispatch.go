package jobs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.DispatchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.dispatchAll(ctx)
		}
	}
}

func (s *Service) dispatchAll(ctx context.Context) {
	s.mu.Lock()
	poolIDs := make([]string, 0, len(s.queues))
	for poolID := range s.queues {
		poolIDs = append(poolIDs, poolID)
	}
	s.mu.Unlock()
	for _, poolID := range poolIDs {
		s.dispatchPool(ctx, poolID)
	}
}

func (s *Service) dispatchPool(ctx context.Context, poolID string) {
	for {
		s.mu.Lock()
		q := s.queues[poolID]
		if len(q) == 0 {
			s.mu.Unlock()
			return
		}
		jobID := q[0]
		s.mu.Unlock()

		job, err := s.st.Jobs().GetJob(ctx, jobID)
		if err != nil || job.Status != store.JobStatusQueued || job.NodeID != "" {
			s.popQueue(poolID, jobID)
			continue
		}
		nodeID, gpus := s.pickNode(ctx, poolID, job)
		if nodeID == "" {
			return
		}
		payload, err := s.startPayload(job, gpus)
		if err != nil {
			s.cfg.Logger.Error("start payload", "job_id", jobID, "err", err)
			s.popQueue(poolID, jobID)
			continue
		}
		if _, err := s.sender.SendCommand(ctx, nodeID, protocol.TypeJobStart, payload, ""); err != nil {
			s.cfg.Logger.Warn("dispatch failed", "job_id", jobID, "node_id", nodeID, "err", err)
			return
		}
		gpusJSON, _ := json.Marshal(gpus)
		if err := s.st.Jobs().AssignJob(ctx, jobID, nodeID, gpusJSON); err != nil {
			s.cfg.Logger.Error("assign job", "job_id", jobID, "err", err)
		}
		s.mu.Lock()
		s.dispatched[jobID] = alloc{nodeID: nodeID, gpus: gpus}
		s.pending[nodeID] = append(s.pending[nodeID], gpus...)
		s.mu.Unlock()
		s.popQueue(poolID, jobID)
		s.cfg.Logger.Info("dispatched", "job_id", jobID, "node_id", nodeID, "gpus", gpus)
	}
}

func (s *Service) popQueue(poolID, jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.queues[poolID]
	if len(q) > 0 && q[0] == jobID {
		s.queues[poolID] = q[1:]
	}
}

func (s *Service) pickNode(ctx context.Context, poolID string, job *store.Job) (string, []int) {
	var req SubmitRequest
	if err := json.Unmarshal(job.Request, &req); err != nil {
		return "", nil
	}
	need := req.GPUs
	if need <= 0 {
		need = 1
	}
	nodeIDs, err := s.st.Pools().NodesOf(ctx, poolID)
	if err != nil {
		return "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, nodeID := range nodeIDs {
		if !s.sender.Online(nodeID) {
			continue
		}
		n, err := s.st.Nodes().Get(ctx, nodeID)
		if err != nil || n.Status != store.NodeStatusOnline {
			continue
		}
		free := subtract(s.freeGPUs[nodeID], s.pending[nodeID])
		if len(free) < need {
			continue
		}
		if req.GPUModel != "" && !nodeHasModel(n.GPUs, req.GPUModel, free[:need]) {
			continue
		}
		return nodeID, free[:need]
	}
	return "", nil
}

func subtract(free, used []int) []int {
	out := make([]int, 0, len(free))
	for _, f := range free {
		skip := false
		for _, u := range used {
			if f == u {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, f)
		}
	}
	return out
}

func nodeHasModel(gpusJSON []byte, model string, idxs []int) bool {
	var gpus []protocol.GPUInfo
	if json.Unmarshal(gpusJSON, &gpus) != nil {
		return false
	}
	byIndex := make(map[int]string, len(gpus))
	for _, g := range gpus {
		byIndex[g.Index] = g.Model
	}
	for _, i := range idxs {
		if byIndex[i] != model {
			return false
		}
	}
	return true
}

func (s *Service) startPayload(job *store.Job, gpus []int) (protocol.JobStart, error) {
	var assets []protocol.Asset
	if err := json.Unmarshal(job.Assets, &assets); err != nil {
		return protocol.JobStart{}, err
	}
	return protocol.JobStart{
		JobID:         job.JobID,
		Engine:        job.Engine,
		EngineVersion: job.EngineVersion,
		ConfigTOML:    job.ConfigTOML,
		Assets:        assets,
		GPUs:          gpus,
		Lane:          protocol.LaneCompute,
	}, nil
}
