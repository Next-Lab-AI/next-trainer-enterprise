package pools

import (
	"context"
	"errors"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

var ErrForbidden = errors.New("pools: forbidden")

type Service struct {
	st store.Store
}

func New(st store.Store) *Service {
	return &Service{st: st}
}

func (s *Service) CanSubmit(ctx context.Context, user *store.User, poolID string) (bool, error) {
	if user.Role == store.RoleAdmin {
		return true, nil
	}
	ids, err := s.st.Pools().PoolsOfUser(ctx, user.ID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == poolID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) CanUseDataset(ctx context.Context, datasetID, poolID string) (bool, error) {
	ids, err := s.st.Pools().PoolsOfDataset(ctx, datasetID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == poolID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) VisiblePools(ctx context.Context, user *store.User) ([]*store.Pool, error) {
	if user.Role == store.RoleAdmin {
		return s.st.Pools().List(ctx)
	}
	ids, err := s.st.Pools().PoolsOfUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out := make([]*store.Pool, 0, len(ids))
	for _, id := range ids {
		p, err := s.st.Pools().Get(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Service) CheckSubmit(ctx context.Context, user *store.User, poolID string, datasetIDs []string) error {
	if user.Role == store.RoleAdmin {
		return nil
	}
	ok, err := s.CanSubmit(ctx, user, poolID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	for _, dsID := range datasetIDs {
		ok, err := s.CanUseDataset(ctx, dsID, poolID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrForbidden
		}
	}
	return nil
}
