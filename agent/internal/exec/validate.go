package exec

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func ValidateConfigPaths(tomlData []byte, agentRoot string, allowPaths ...string) error {
	var doc map[string]any
	if err := toml.Unmarshal(tomlData, &doc); err != nil {
		return fmt.Errorf("exec: config toml parse: %w", err)
	}
	return walkStrings(doc, func(key, val string) error {
		if !looksLikePath(val) {
			return nil
		}
		if filepath.IsAbs(val) {
			clean := filepath.Clean(val)
			if underDir(agentRoot, clean) {
				return nil
			}
			for _, allow := range allowPaths {
				if underDir(allow, clean) {
					return nil
				}
			}
			return fmt.Errorf("exec: config path %q (%s) escapes agent root", val, key)
		}
		for _, seg := range strings.Split(filepath.ToSlash(val), "/") {
			if seg == ".." {
				return fmt.Errorf("exec: config path %q (%s) contains '..'", val, key)
			}
		}
		return nil
	})
}

func underDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func looksLikePath(s string) bool {
	return filepath.IsAbs(s) || strings.Contains(s, "/") || strings.Contains(s, "..")
}

func walkStrings(v any, fn func(key, val string) error) error {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if err := walkKeyed(k, x, fn); err != nil {
				return err
			}
		}
	case []any:
		for _, x := range t {
			if err := walkStrings(x, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

func walkKeyed(key string, v any, fn func(key, val string) error) error {
	if s, ok := v.(string); ok {
		return fn(key, s)
	}
	return walkStrings(v, fn)
}
