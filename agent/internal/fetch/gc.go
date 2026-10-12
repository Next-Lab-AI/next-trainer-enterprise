package fetch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (f *Fetcher) GC(ctx context.Context, olderThanDays int) ([]string, error) {
	if olderThanDays <= 0 {
		olderThanDays = 30
	}
	cutoff := time.Now().Add(-time.Duration(olderThanDays) * 24 * time.Hour)
	referenced := f.referencedCaches()
	dir := filepath.Join(f.root, "datasets")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if referenced[full] {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().After(cutoff) {
			continue
		}
		if err := os.RemoveAll(full); err != nil {
			return removed, err
		}
		removed = append(removed, e.Name())
	}
	return removed, nil
}

func (f *Fetcher) referencedCaches() map[string]bool {
	out := map[string]bool{}
	jobsDir := filepath.Join(f.root, "jobs")
	jobEntries, err := os.ReadDir(jobsDir)
	if err != nil {
		return out
	}
	prefix := filepath.Join(f.root, "datasets") + string(filepath.Separator)
	for _, je := range jobEntries {
		filepath.WalkDir(filepath.Join(jobsDir, je.Name()), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				link, err := os.Readlink(p)
				if err != nil {
					return nil
				}
				if !filepath.IsAbs(link) {
					link, err = filepath.Abs(filepath.Join(filepath.Dir(p), link))
					if err != nil {
						return nil
					}
				}
				if strings.HasPrefix(link, prefix) {
					out[link] = true
				}
			}
			return nil
		})
	}
	return out
}
