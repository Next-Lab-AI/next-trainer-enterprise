package registry

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

var (
	ErrBadToken     = errors.New("registry: bad token")
	ErrRevoked      = errors.New("registry: node revoked")
	ErrWindowClosed = errors.New("registry: enrollment window closed")
)

const DefaultPool = "default"

type Registry struct {
	st   store.Store
	auth *auth.Service
	now  func() time.Time
}

type HelloResult struct {
	Node       *store.Node
	Credential string
	NewNode    bool
}

func New(st store.Store, authSvc *auth.Service) *Registry {
	return &Registry{st: st, auth: authSvc, now: time.Now}
}

func (r *Registry) HandleHello(ctx context.Context, h protocol.Hello) (*HelloResult, error) {
	if h.NodeID != "" || h.Credential != "" {
		return r.resume(ctx, h)
	}
	if h.AccessToken != "" {
		if _, err := r.auth.ValidateAccessToken(ctx, h.AccessToken); err != nil {
			return nil, ErrBadToken
		}
		return r.register(ctx, h)
	}
	open, err := r.auth.RegistrationOpen(ctx)
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, ErrWindowClosed
	}
	return r.register(ctx, h)
}

func (r *Registry) resume(ctx context.Context, h protocol.Hello) (*HelloResult, error) {
	n, err := r.st.Nodes().Get(ctx, h.NodeID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, ErrBadToken
		}
		return nil, err
	}
	if n.Status == store.NodeStatusRevoked {
		return nil, ErrRevoked
	}
	if n.CredentialHash != auth.HashToken(h.Credential) {
		return nil, ErrBadToken
	}
	if h.Hostname != "" {
		n.Hostname = h.Hostname
	}
	if err := r.refreshFacts(ctx, n, h); err != nil {
		return nil, err
	}
	if err := r.st.Nodes().UpdateHeartbeat(ctx, n.NodeID, r.now().Unix(), store.NodeStatusOnline); err != nil {
		return nil, err
	}
	return &HelloResult{Node: n}, nil
}

func (r *Registry) register(ctx context.Context, h protocol.Hello) (*HelloResult, error) {
	credential := auth.NewToken()
	labels, _ := json.Marshal(h.Labels)
	gpus, _ := json.Marshal(h.GPUs)
	now := r.now().Unix()
	n := &store.Node{
		NodeID:          store.NewID(),
		CredentialHash:  auth.HashToken(credential),
		Hostname:        h.Hostname,
		Description:     h.Description,
		AgentVersion:    h.AgentVersion,
		ProtocolVersion: strconv.Itoa(protocol.Version),
		Labels:          labels,
		GPUs:            gpus,
		Status:          store.NodeStatusOnline,
		FirstSeen:       now,
		LastSeen:        now,
	}
	poolName := h.Pool
	if poolName == "" {
		poolName = DefaultPool
	}
	err := r.st.Tx(ctx, func(s store.Store) error {
		if err := s.Nodes().Create(ctx, n); err != nil {
			return err
		}
		pool, err := s.Pools().GetByName(ctx, poolName)
		if err == store.ErrNotFound {
			pool = &store.Pool{PoolID: store.NewID(), Name: poolName, AutoCreated: true, CreatedAt: now}
			if err := s.Pools().Create(ctx, pool); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		return s.Pools().AssignNode(ctx, n.NodeID, pool.PoolID)
	})
	if err != nil {
		return nil, err
	}
	return &HelloResult{Node: n, Credential: credential, NewNode: true}, nil
}

func (r *Registry) refreshFacts(ctx context.Context, n *store.Node, h protocol.Hello) error {
	labels, _ := json.Marshal(h.Labels)
	gpus, _ := json.Marshal(h.GPUs)
	return r.st.Nodes().UpdateFacts(ctx, n.NodeID, n.Hostname, h.AgentVersion,
		strconv.Itoa(protocol.Version), labels, gpus)
}

func (r *Registry) Revoke(ctx context.Context, nodeID string) error {
	return r.st.Nodes().SetStatus(ctx, nodeID, store.NodeStatusRevoked)
}

func (r *Registry) MarkOffline(ctx context.Context, nodeID string) error {
	return r.st.Nodes().MarkOffline(ctx, nodeID)
}

func (r *Registry) Touch(ctx context.Context, nodeID string) error {
	return r.st.Nodes().UpdateHeartbeat(ctx, nodeID, r.now().Unix(), store.NodeStatusOnline)
}
