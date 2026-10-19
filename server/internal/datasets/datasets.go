package datasets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

var (
	ErrBadKind    = errors.New("datasets: bad kind")
	ErrBadBackend = errors.New("datasets: unknown backend")
	ErrDeleted    = errors.New("datasets: deleted")
	ErrForbidden  = errors.New("datasets: forbidden")
)

type Service struct {
	st       store.Store
	backends map[string]Backend
	now      func() int64
}

func New(st store.Store, backends ...Backend) *Service {
	s := &Service{
		st:       st,
		backends: make(map[string]Backend),
		now:      unixNow,
	}
	for _, b := range backends {
		s.backends[b.Name()] = b
	}
	return s
}

func (s *Service) backend(name string) (Backend, error) {
	b, ok := s.backends[name]
	if !ok {
		return nil, ErrBadBackend
	}
	return b, nil
}

func validUploadKind(kind string) bool {
	return kind == store.DatasetKindDataset
}

func sanitizeName(name string) string {
	name = path.Base(name)
	name = strings.ReplaceAll(name, "..", "_")
	if name == "" || name == "." || name == "/" {
		return "blob"
	}
	return name
}

// EncAlgoAESZip marks datasets stored as a password-protected zip (WinZip
// AES-256). The key is kept in the DB and handed to the agent at dispatch.
const EncAlgoAESZip = "zip-aes256"

func (s *Service) Upload(ctx context.Context, userID, name, version, kind, backendName, filename, encAlgo, encKey string, r io.Reader) (*store.Dataset, error) {
	if !validUploadKind(kind) {
		return nil, ErrBadKind
	}
	if encKey == "" {
		encAlgo = ""
	} else if encAlgo == "" {
		encAlgo = EncAlgoAESZip
	}
	if backendName == "" {
		backendName = store.BackendLocal
	}
	b, err := s.backend(backendName)
	if err != nil {
		return nil, err
	}
	id := store.NewID()
	relURI := path.Join("datasets", id, sanitizeName(filename))
	sum, size, err := b.Put(ctx, relURI, r)
	if err != nil {
		return nil, err
	}
	d := &store.Dataset{
		DatasetID: id, Name: name, Version: version, Kind: kind,
		Backend: backendName, URI: relURI, SHA256: sum, SizeBytes: size,
		CreatedBy: userID, CreatedAt: s.now(), EncAlgo: encAlgo, EncKey: encKey,
	}
	if err := s.st.Datasets().Create(ctx, d); err != nil {
		b.Delete(ctx, relURI)
		return nil, err
	}
	return d, nil
}

func (s *Service) Download(ctx context.Context, datasetID string) (io.ReadCloser, *store.Dataset, error) {
	d, err := s.st.Datasets().Get(ctx, datasetID)
	if err != nil {
		return nil, nil, err
	}
	if d.DeletedAt != nil {
		return nil, nil, ErrDeleted
	}
	b, err := s.backend(d.Backend)
	if err != nil {
		return nil, nil, err
	}
	rc, err := b.Open(ctx, d.URI)
	if err != nil {
		return nil, nil, err
	}
	return rc, d, nil
}

func (s *Service) CanAgentDownload(ctx context.Context, datasetID, jobID string) (bool, error) {
	job, err := s.st.Jobs().GetJob(ctx, jobID)
	if err != nil {
		return false, err
	}
	poolsOfDataset, err := s.st.Pools().PoolsOfDataset(ctx, datasetID)
	if err != nil {
		return false, err
	}
	for _, p := range poolsOfDataset {
		if p == job.PoolID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) ListArtifacts(ctx context.Context, jobID string) ([]*store.Dataset, error) {
	return s.st.Datasets().ListByJob(ctx, jobID)
}

// CanUserDownload: admin 或（用户所在池 ∩ 数据集授权池）非空。
func (s *Service) CanUserDownload(ctx context.Context, user *store.User, datasetID string) (bool, error) {
	if user.Role == store.RoleAdmin {
		return true, nil
	}
	userPools, err := s.st.Pools().PoolsOfUser(ctx, user.ID)
	if err != nil {
		return false, err
	}
	dsPools, err := s.st.Pools().PoolsOfDataset(ctx, datasetID)
	if err != nil {
		return false, err
	}
	set := map[string]bool{}
	for _, p := range userPools {
		set[p] = true
	}
	for _, p := range dsPools {
		if set[p] {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) ReceiveArtifact(ctx context.Context, jobID, relPath string, r io.Reader) (*store.Dataset, error) {
	job, err := s.st.Jobs().GetJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("datasets: artifact for unknown job %s", jobID)
	}
	clean, err := cleanRel(relPath)
	if err != nil {
		return nil, err
	}
	b, err := s.backend(store.BackendLocal)
	if err != nil {
		return nil, err
	}
	relURI := path.Join("artifacts", jobID, clean)
	sum, size, err := b.Put(ctx, relURI, r)
	if err != nil {
		return nil, err
	}
	d := &store.Dataset{
		DatasetID: store.NewID(), Name: jobID + "/" + clean, Kind: store.DatasetKindArtifact,
		Backend: store.BackendLocal, URI: relURI, SHA256: sum, SizeBytes: size,
		CreatedBy: job.UserID, CreatedAt: s.now(), JobID: jobID,
	}
	if err := s.st.Datasets().Create(ctx, d); err != nil {
		b.Delete(ctx, relURI)
		return nil, err
	}
	if err := s.st.Pools().GrantDataset(ctx, d.DatasetID, job.PoolID); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) Delete(ctx context.Context, datasetID string) error {
	d, err := s.st.Datasets().Get(ctx, datasetID)
	if err != nil {
		return err
	}
	if err := s.st.Datasets().SoftDelete(ctx, datasetID, s.now()); err != nil {
		return err
	}
	if b, err := s.backend(d.Backend); err == nil {
		b.Delete(ctx, d.URI)
	}
	return nil
}
