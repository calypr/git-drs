package pathspec

import "testing"

func TestMatches(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{pattern: "data/**", path: "data/a/b/file.bin", want: true},
		{pattern: "*.bam", path: "data/file.bam", want: false},
		{pattern: "data/*.bam", path: "data/file.bam", want: true},
		{pattern: "a/file.txt", path: "a/file.txt", want: true},
		{pattern: "a/file.txt", path: "a/other.txt", want: false},
	}
	for _, tc := range cases {
		if got := Matches(tc.path, tc.pattern); got != tc.want {
			t.Fatalf("Matches(%q, %q) = %v, want %v", tc.path, tc.pattern, got, tc.want)
		}
	}
}
