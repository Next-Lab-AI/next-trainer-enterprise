package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/engines"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/pools"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

var (
	ErrBadRequest = errors.New("jobs: bad request")
	ErrForbidden  = errors.New("jobs: forbidden")
	ErrNotRunning = errors.New("jobs: job not running")
)

type CommandSender interface {
	SendCommand(ctx context.Context, nodeID, msgType string, payload any, issuedBy string) (string, error)
	Online(nodeID string) bool
}

type Config struct {
	LogDir           string
	HTTPBase         string
	DispatchInterval time.Duration
	Logger           *slog.Logger
}

type SubmitRequest struct {
	PoolID        string         `json:"pool_id"`
	Engine        string         `json:"engine"`
	EngineVersion string         `json:"engine_version"`
	Params        engines.Params `json:"params"`
	DatasetIDs    []string       `json:"dataset_ids"`
	GPUs          int            `json:"gpus"`
	GPUModel      string         `json:"gpu_model,omitempty"`
}

type Service struct {
	st     store.Store
	pools  *pools.Service
	sender CommandSender
	cfg    Config
	now    func() time.Time

	mu         sync.Mutex
	queues     map[string][]string
	freeGPUs   map[string][]int
	pending    map[string][]int
	dispatched map[string]alloc
}

type alloc struct {
	nodeID string
	gpus   []int
}

func New(st store.Store, poolsSvc *pools.Service, sender CommandSender, cfg Config) *Service {
	if cfg.DispatchInterval <= 0 {
		cfg.DispatchInterval = 500 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{
		st:         st,
		pools:      poolsSvc,
		sender:     sender,
		cfg:        cfg,
		now:        time.Now,
		queues:     make(map[string][]string),
		freeGPUs:   make(map[string][]int),
		pending:    make(map[string][]int),
		dispatched: make(map[string]alloc),
	}
}

func (s *Service) Submit(ctx context.Context, user *store.User, req SubmitRequest) (*store.Job, error) {
	if req.GPUs <= 0 {
		req.GPUs = 1
	}
	if !engines.Supported(req.Engine) {
		return nil, fmt.Errorf("%w: unsupported engine %q", ErrBadRequest, req.Engine)
	}
	if _, err := s.st.Pools().Get(ctx, req.PoolID); err != nil {
		return nil, fmt.Errorf("%w: unknown pool", ErrBadRequest)
	}
	if err := s.pools.CheckSubmit(ctx, user, req.PoolID, req.DatasetIDs); err != nil {
		return nil, ErrForbidden
	}
	jobID := store.NewID()
	cfgTOML, err := engines.Render(req.Engine, req.EngineVersion, req.Params, jobID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	assets, err := s.buildAssets(ctx, req.DatasetIDs)
	if err != nil {
		return nil, err
	}
	reqJSON, _ := json.Marshal(req)
	assetsJSON, _ := json.Marshal(assets)
	job := &store.Job{
		JobID: jobID, UserID: user.ID, PoolID: req.PoolID,
		Engine: req.Engine, EngineVersion: req.EngineVersion,
		ConfigTOML: cfgTOML, Assets: assetsJSON, Request: reqJSON,
		Status: store.JobStatusQueued, CreatedAt: s.now().Unix(),
	}
	if err := s.st.Jobs().CreateJob(ctx, job); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.queues[req.PoolID] = append(s.queues[req.PoolID], jobID)
	s.mu.Unlock()
	return job, nil
}

func (s *Service) buildAssets(ctx context.Context, datasetIDs []string) ([]protocol.Asset, error) {
	var out []protocol.Asset
	for _, id := range datasetIDs {
		d, err := s.st.Datasets().Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("%w: unknown dataset %s", ErrBadRequest, id)
		}
		if d.DeletedAt != nil {
			return nil, fmt.Errorf("%w: dataset %s deleted", ErrBadRequest, id)
		}
		uri := d.URI
		if d.Backend == store.BackendLocal {
			uri = strings.TrimSuffix(s.cfg.HTTPBase, "/") + "/datasets/" + d.DatasetID
		}
		out = append(out, protocol.Asset{
			URI:    uri,
			Dest:   "datasets/" + d.Name,
			SHA256: d.SHA256,
			Size:   d.SizeBytes,
		})
	}
	return out, nil
}

func (s *Service) canOperate(user *store.User, job *store.Job) bool {
	return user.Role == store.RoleAdmin || job.UserID == user.ID
}

func (s *Service) Stop(ctx context.Context, user *store.User, jobID string, preempt bool) error {
	job, err := s.st.Jobs().GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if preempt && user.Role != store.RoleAdmin {
		return ErrForbidden
	}
	if !s.canOperate(user, job) {
		return ErrForbidden
	}
	if job.Status != store.JobStatusRunning && job.Status != store.JobStatusWaiting && job.Status != store.JobStatusCreated {
		return ErrNotRunning
	}
	_, err = s.sender.SendCommand(ctx, job.NodeID, protocol.TypeJobStop,
		protocol.JobStop{JobID: jobID, Preempt: preempt, GraceS: 60}, user.ID)
	return err
}

func (s *Service) Kill(ctx context.Context, user *store.User, jobID string) error {
	job, err := s.st.Jobs().GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if !s.canOperate(user, job) {
		return ErrForbidden
	}
	if job.Status != store.JobStatusRunning && job.Status != store.JobStatusWaiting && job.Status != store.JobStatusCreated {
		return ErrNotRunning
	}
	_, err = s.sender.SendCommand(ctx, job.NodeID, protocol.TypeJobKill,
		protocol.JobKill{JobID: jobID}, user.ID)
	return err
}

func (s *Service) List(ctx context.Context, user *store.User, f store.JobFilter) ([]*store.Job, error) {
	if user.Role == store.RoleAdmin {
		return s.st.Jobs().ListJobs(ctx, f)
	}
	granted, err := s.st.Pools().PoolsOfUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	inPool := make(map[string]bool, len(granted))
	for _, id := range granted {
		inPool[id] = true
	}
	all, err := s.st.Jobs().ListJobs(ctx, f)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, j := range all {
		if j.UserID == user.ID || inPool[j.PoolID] {
			out = append(out, j)
		}
	}
	return out, nil
}

func (s *Service) RebuildQueues(ctx context.Context) error {
	queued, err := s.st.Jobs().ListJobs(ctx, store.JobFilter{Status: []string{store.JobStatusQueued}})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queues = make(map[string][]string)
	s.dispatched = make(map[string]alloc)
	for i := len(queued) - 1; i >= 0; i-- {
		j := queued[i]
		if j.NodeID != "" {
			var gpus []int
			json.Unmarshal(j.GPUs, &gpus)
			s.dispatched[j.JobID] = alloc{nodeID: j.NodeID, gpus: gpus}
			continue
		}
		s.queues[j.PoolID] = append(s.queues[j.PoolID], j.JobID)
	}
	return nil
}

func (s *Service) logFile(jobID string) (*os.File, error) {
	dir := filepath.Join(s.cfg.LogDir, "jobs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, jobID+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}
