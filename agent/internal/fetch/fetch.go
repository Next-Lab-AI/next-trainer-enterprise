package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

type Fetcher struct {
	root         string
	serverBase   string
	sessionToken func() string
	s3Config     func() S3Config
	HTTPClient   *http.Client
}

type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
}

func New(root, serverBase string, sessionToken func() string, s3Config func() S3Config) *Fetcher {
	return &Fetcher{root: root, serverBase: strings.TrimSuffix(serverBase, "/"), sessionToken: sessionToken, s3Config: s3Config}
}

func S3ConfigFromEnv() S3Config {
	return S3Config{
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		Region:    os.Getenv("S3_REGION"),
	}
}

func cacheKey(uri string) string {
	sum := sha256.Sum256([]byte(uri))
	return hex.EncodeToString(sum[:])[:16]
}

func (f *Fetcher) CachePath(uri string) string {
	return filepath.Join(f.root, "datasets", cacheKey(uri))
}

func (f *Fetcher) Cached(uri, wantSHA256 string) (string, bool) {
	path := f.CachePath(uri) + ".data"
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	if wantSHA256 == "" {
		return path, true
	}
	sum, err := hashFile(path)
	if err != nil || sum != wantSHA256 {
		return "", false
	}
	return path, true
}

func (f *Fetcher) Fetch(ctx context.Context, a protocol.Asset) (cachePath, sha256Hex string, err error) {
	if p, ok := f.Cached(a.URI, a.SHA256); ok {
		return p, a.SHA256, nil
	}
	u, err := url.Parse(a.URI)
	if err != nil {
		return "", "", fmt.Errorf("fetch: bad uri: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Join(f.root, "datasets"), ".dl-*.tmp")
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Join(f.root, "datasets"), 0o755); mkErr != nil {
			return "", "", fmt.Errorf("fetch: mkdir datasets: %w", mkErr)
		}
		tmp, err = os.CreateTemp(filepath.Join(f.root, "datasets"), ".dl-*.tmp")
		if err != nil {
			return "", "", fmt.Errorf("fetch: temp file: %w", err)
		}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	h := sha256.New()
	w := io.MultiWriter(tmp, h)

	switch u.Scheme {
	case "file":
		err = f.fetchFile(ctx, u, w)
	case "https", "http":
		err = f.fetchHTTPS(ctx, u, w)
	case "s3":
		err = f.fetchS3(ctx, u, w)
	default:
		err = fmt.Errorf("fetch: scheme %q not allowed", u.Scheme)
	}
	if err != nil {
		tmp.Close()
		return "", "", err
	}
	if err := tmp.Close(); err != nil {
		return "", "", err
	}

	sum := hex.EncodeToString(h.Sum(nil))
	if a.SHA256 != "" && sum != a.SHA256 {
		return "", "", fmt.Errorf("fetch: sha256 mismatch for %s: got %s want %s", a.URI, sum, a.SHA256)
	}

	final := f.CachePath(a.URI) + ".data"
	if err := os.Rename(tmpPath, final); err != nil {
		return "", "", fmt.Errorf("fetch: commit: %w", err)
	}
	return final, sum, nil
}

func (f *Fetcher) fetchFile(ctx context.Context, u *url.URL, w io.Writer) error {
	src, err := os.Open(u.Path)
	if err != nil {
		return fmt.Errorf("fetch: open %s: %w", u.Path, err)
	}
	defer src.Close()
	_, err = io.Copy(w, src)
	return err
}

func hashFile(path string) (string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func LinkIntoJob(cachePath, jobDir, dest string) (string, error) {
	if dest == "" {
		dest = filepath.Base(cachePath)
	}
	target := filepath.Join(jobDir, dest)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.Symlink(cachePath, target); err != nil {
		return "", fmt.Errorf("fetch: link into job: %w", err)
	}
	return target, nil
}
