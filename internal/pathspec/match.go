package pathspec

import (
	"path/filepath"
	"regexp"
	"strings"
)

func MatchesAny(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	normalized := filepath.ToSlash(filepath.Clean(path))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if Matches(normalized, pattern) {
			return true
		}
	}
	return false
}

func Matches(path, pattern string) bool {
	pattern = filepath.ToSlash(filepath.Clean(pattern))
	if !strings.ContainsAny(pattern, "*?[") {
		return path == pattern
	}
	re, err := regexp.Compile(globToRegexp(pattern))
	if err != nil {
		return false
	}
	return re.MatchString(path)
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteString("$")
	return b.String()
}
