package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	yekazip "github.com/yeka/zip"
)

// UnpackedDir returns the cache directory an encrypted asset is unpacked to.
// The content sha256 is part of the name so changed content never reuses a
// stale directory.
func (f *Fetcher) UnpackedDir(uri, sha256Hex string) string {
	sum := sha256.Sum256([]byte(uri + ":" + sha256Hex))
	return f.CachePath(uri) + "-" + hex.EncodeToString(sum[:])[:8] + ".dir"
}

// EnsureUnpacked decrypts (zip AES) and unpacks the cached zip into its
// unpacked dir, returning the dir path. Idempotent: an existing dir wins.
func (f *Fetcher) EnsureUnpacked(uri, sha256Hex, encKeyB64 string) (string, error) {
	dir := f.UnpackedDir(uri, sha256Hex)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, nil
	}
	zipPath, ok := f.Cached(uri, sha256Hex)
	if !ok {
		return "", fmt.Errorf("fetch: cached zip missing for %s", uri)
	}
	key := encKeyB64 // the key IS the zip password (base64-shaped random string)
	if key == "" {
		return "", fmt.Errorf("fetch: empty enc key for %s", uri)
	}
	tmp := dir + ".tmp-" + hex.EncodeToString(sum4(zipPath))
	if err := os.RemoveAll(tmp); err != nil {
		return "", err
	}
	if err := unzipEncrypted(zipPath, tmp, key); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("fetch: unpack %s: %w", uri, err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("fetch: commit unpack dir: %w", err)
	}
	return dir, nil
}

func sum4(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:4]
}

func unzipEncrypted(zipPath, destDir, password string) error {
	rc, err := yekazip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer rc.Close()
	for _, zf := range rc.File {
		clean, err := cleanZipName(zf.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, clean)
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if zf.IsEncrypted() {
			zf.SetPassword(password)
		}
		src, err := zf.Open()
		if err != nil {
			return fmt.Errorf("open %s: %w", zf.Name, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			src.Close()
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode())
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil {
			return fmt.Errorf("extract %s: %w", zf.Name, copyErr)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func cleanZipName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("fetch: unsafe zip entry %q", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return "", fmt.Errorf("fetch: unsafe zip entry %q", name)
		}
	}
	clean := filepath.Clean("/" + name)
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		return "", fmt.Errorf("fetch: unsafe zip entry %q", name)
	}
	return clean, nil
}
