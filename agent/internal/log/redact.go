package log

import (
	"regexp"
	"sort"
	"strings"
)

var sensitiveKeyRe = regexp.MustCompile(`(?i)(TOKEN|KEY|SECRET|PASSWORD)`)

var kvRedactRe = regexp.MustCompile(`(?i)((?:TOKEN|KEY|SECRET|PASSWORD)["']?\s*[=:]\s*["']?)([^\s"']+)`)

const mask = "***"

type Redactor struct {
	root   string
	values []string
}

func NewRedactor(agentRoot string, sensitiveValues []string) *Redactor {
	vals := make([]string, 0, len(sensitiveValues))
	for _, v := range sensitiveValues {
		if len(v) >= 8 {
			vals = append(vals, v)
		}
	}
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	return &Redactor{root: agentRoot, values: vals}
}

func SensitiveValuesFromEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		key, val, ok := strings.Cut(kv, "=")
		if ok && sensitiveKeyRe.MatchString(key) && val != "" {
			out = append(out, val)
		}
	}
	return out
}

func (r *Redactor) Redact(s string) string {
	for _, v := range r.values {
		if strings.Contains(s, v) {
			s = strings.ReplaceAll(s, v, mask)
		}
	}
	s = kvRedactRe.ReplaceAllString(s, "${1}"+mask)
	if r.root != "" {
		s = strings.ReplaceAll(s, r.root+"/", "")
	}
	return s
}
