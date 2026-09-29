package pathspec

import "testing"

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		pattern string
		want    bool
	}{
		{name: "exact path", path: "data/file.txt", pattern: "data/file.txt", want: true},
		{name: "exact path mismatch", path: "data/other.txt", pattern: "data/file.txt", want: false},
		{name: "custom double star", path: "data/nested/file.txt", pattern: "data/**", want: true},
		{name: "custom double star at root", path: "data/file.txt", pattern: "data/**", want: true},
		{name: "single star stays within a directory", path: "data/file.txt", pattern: "data/*.txt", want: true},
		{name: "single star does not cross a slash", path: "data/nested/file.txt", pattern: "data/*.txt", want: false},
		{name: "question mark matches one non-slash character", path: "data/a.txt", pattern: "data/?.txt", want: true},
		{name: "question mark does not match a slash", path: "data/nested.txt", pattern: "data/?.txt", want: false},
		{name: "special path characters stay literal", path: "data/v1.0+file.txt", pattern: "data/v1.0+file.txt", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := MatchesPattern(test.path, test.pattern); got != test.want {
				t.Fatalf("MatchesPattern(%q, %q) = %v, want %v", test.path, test.pattern, got, test.want)
			}
		})
	}
}

func TestMatchesAnyPattern(t *testing.T) {
	for _, test := range []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{name: "no patterns match everything", path: "data/file.txt", patterns: nil, want: true},
		{name: "empty patterns are ignored", path: "data/file.txt", patterns: []string{"", "  ", "data/*.txt"}, want: true},
		{name: "none match", path: "data/file.txt", patterns: []string{"docs/**", "other.txt"}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := MatchesAnyPattern(test.path, test.patterns); got != test.want {
				t.Fatalf("MatchesAnyPattern(%q, %q) = %v, want %v", test.path, test.patterns, got, test.want)
			}
		})
	}
}
