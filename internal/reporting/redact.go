package reporting

import "regexp"

var secretPatterns = []*regexp.Regexp{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{20,}`), regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`), regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer|token)\s+)\S+`)}

func Redact(value string) string {
	for _, pattern := range secretPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}
