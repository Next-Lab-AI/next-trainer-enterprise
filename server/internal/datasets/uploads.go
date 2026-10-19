package datasets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

var (
	ErrUploadNotFound = errors.New("datasets: upload session not found")
	ErrUploadOffset   = errors.New("datasets: chunk offset mismatch")
	ErrUploadSize     = errors.New("datasets: assembled size mismatch")
)

const uploadSessionTTL = 24 * time.Hour

// UploadSession tracks one chunked upload; state lives in a sidecar file so
// sessions survive server restarts.
type UploadSession struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Filename  string `json:"filename"`
	EncAlgo   string `json:"enc_algo"`
	EncKey    string `json:"enc_key"`
	Size      int64  `json:"size"`
	Received  int64  `json:"received"`
	CreatedAt int64  `json:"created_at"`
}

type UploadManager struct {
	dir string
	svc *Service
	mu  sync.Mutex
	now func() int64
}

func NewUploadManager(dataRoot string, svc *Service) *UploadManager {
	return &UploadManager{dir: filepath.Join(dataRoot, "uploads"), svc: svc, now: unixNow}
}

func (m *UploadManager) Init(userID, name, version, filename, encAlgo, encKey string, size int64) (*UploadSession, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: size must be positive", ErrUploadSize)
	}
	s := &UploadSession{
		ID: store.NewID(), UserID: userID, Name: name, Version: version,
		Filename: filename, EncAlgo: encAlgo, EncKey: encKey, Size: size,
		CreatedAt: m.now(),
	}
	if err := os.MkdirAll(m.sessionDir(s.ID), 0o755); err != nil {
		return nil, err
	}
	if err := m.save(s); err != nil {
		return nil, err
	}
	return s, nil
}

func (m *UploadManager) WriteChunk(id string, offset int64, r io.Reader) (*UploadSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.load(id)
	if err != nil {
		return nil, err
	}
	if offset != s.Received {
		return s, ErrUploadOffset
	}
	part := filepath.Join(m.sessionDir(id), "part")
	fh, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	if _, err := fh.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	n, err := io.Copy(fh, r)
	if err != nil {
		return nil, err
	}
	s.Received += n
	if s.Received > s.Size {
		return nil, ErrUploadSize
	}
	if err := m.save(s); err != nil {
		return nil, err
	}
	return s, nil
}

func (m *UploadManager) Complete(ctx context.Context, id string) (*store.Dataset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.load(id)
	if err != nil {
		return nil, err
	}
	if s.Received != s.Size {
		return nil, fmt.Errorf("%w: got %d want %d", ErrUploadSize, s.Received, s.Size)
	}
	part := filepath.Join(m.sessionDir(id), "part")
	fh, err := os.Open(part)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	d, err := m.svc.Upload(ctx, s.UserID, s.Name, s.Version, store.DatasetKindDataset, "", s.Filename, s.EncAlgo, s.EncKey, fh)
	if err != nil {
		return nil, err
	}
	os.RemoveAll(m.sessionDir(id))
	return d, nil
}

func (m *UploadManager) Get(id string) (*UploadSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.load(id)
}

// GC drops sessions idle past the TTL; call periodically.
func (m *UploadManager) GC() {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	cutoff := m.now() - int64(uploadSessionTTL/time.Second)
	for _, e := range entries {
		s, err := m.load(e.Name())
		if err != nil || s.CreatedAt < cutoff {
			os.RemoveAll(filepath.Join(m.dir, e.Name()))
		}
	}
}

func (m *UploadManager) sessionDir(id string) string {
	return filepath.Join(m.dir, id)
}

func (m *UploadManager) load(id string) (*UploadSession, error) {
	raw, err := os.ReadFile(filepath.Join(m.sessionDir(id), "meta.json"))
	if os.IsNotExist(err) {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, err
	}
	var s UploadSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (m *UploadManager) save(s *UploadSession) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.sessionDir(s.ID), "meta.json"), raw, 0o644)
}
