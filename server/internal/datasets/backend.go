package datasets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Backend interface {
	Name() string
	Put(ctx context.Context, relURI string, r io.Reader) (sha256Hex string, size int64, err error)
	Open(ctx context.Context, relURI string) (io.ReadCloser, error)
	Delete(ctx context.Context, relURI string) error
}

type LocalBackend struct {
	root string
}

func NewLocal(root string) (*LocalBackend, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalBackend{root: root}, nil
}

func (b *LocalBackend) Name() string { return "local" }

func cleanRel(relURI string) (string, error) {
	for _, seg := range strings.Split(relURI, "/") {
		if seg == ".." {
			return "", fmt.Errorf("datasets: bad uri %q", relURI)
		}
	}
	clean := filepath.Clean("/" + relURI)
	return strings.TrimPrefix(clean, "/"), nil
}

func (b *LocalBackend) path(relURI string) (string, error) {
	clean, err := cleanRel(relURI)
	if err != nil {
		return "", err
	}
	return filepath.Join(b.root, clean), nil
}

func (b *LocalBackend) Put(ctx context.Context, relURI string, r io.Reader) (string, int64, error) {
	path, err := b.path(relURI)
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", 0, err
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return "", 0, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func (b *LocalBackend) Open(ctx context.Context, relURI string) (io.ReadCloser, error) {
	path, err := b.path(relURI)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (b *LocalBackend) Delete(ctx context.Context, relURI string) error {
	path, err := b.path(relURI)
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}
