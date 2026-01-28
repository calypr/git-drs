package prepush

import (
	"os"
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestPrepushCmd(t *testing.T) {
	testutils.TestCmdMain(t, "prepush")
}

func TestValidateArgs(t *testing.T) {
	testutils.TestCmdArgs(t)
}

func TestReadPushedBranches(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string // Sorted
	}{
		{
			name:     "single branch",
			input:    "refs/heads/main 1234 oid123 refs/heads/main 1234 oid456",
			expected: []string{"main"},
		},
		{
			name:     "multiple branches",
			input:    "refs/heads/main 123 oid refs/heads/main 456 oid\nrefs/heads/feature/foo 789 oid remote 000 oid",
			expected: []string{"feature/foo", "main"},
		},
		{
			name:     "ignore tags",
			input:    "refs/tags/v1.0 123 oid refs/tags/v1.0 123 oid",
			expected: []string{},
		},
		{
			name:     "empty input",
			input:    "",
			expected: []string{},
		},
		{
			name:     "malformed lines",
			input:    "just-garbage\nrefs/heads/ok 1 2 3",
			expected: []string{"ok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp, err := os.CreateTemp("", "test-stdin")
			if err != nil {
				t.Fatalf("create temp: %v", err)
			}
			defer os.Remove(tmp.Name())

			if _, err := tmp.WriteString(tt.input); err != nil {
				t.Fatalf("write temp: %v", err)
			}

			// readPushedBranches seeks to 0 itself, but we pass the *os.File
			// which must be valid.
			branches, err := readPushedBranches(tmp)
			if err != nil {
				t.Fatalf("readPushedBranches error: %v", err)
			}

			if len(branches) != len(tt.expected) {
				t.Errorf("expected %d branches, got %d: %v", len(tt.expected), len(branches), branches)
				return
			}
			for i := range branches {
				if branches[i] != tt.expected[i] {
					t.Errorf("branch mismatch at %d: got %s, want %s", i, branches[i], tt.expected[i])
				}
			}

			tmp.Close()
		})
	}
}
