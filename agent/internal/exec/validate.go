package exec

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func ValidateConfigPaths(tomlData []byte, agentRoot string) error {
	var doc map[string]any
	if err := toml.Unmarshal(tomlData, &doc); err != nil {
		return fmt.Errorf("exec: config toml parse: %w", err)
	}
	return walkStrings(doc, func(key, val string) error {
		if !looksLikePath(val) {
			return nil
		}
		if filepath.IsAbs(val) {
			rel, err := filepath.Rel(agentRoot, filepath.Clean(val))
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("exec: config path %q (%s) escapes agent root", val, key)
			}
			return nil
		}
		for _, seg := range strings.Split(filepath.ToSlash(val), "/") {
			if seg == ".." {
				return fmt.Errorf("exec: config path %q (%s) contains '..'", val, key)
			}
		}
		return nil
	})
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
