package untrack

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunUntrack_RemovesPatternsWithFlags(t *testing.T) {
	origUntrack := gitLFSUntrackPatterns
	t.Cleanup(func() {
		gitLFSUntrackPatterns = origUntrack
	})

	called := false
	gitLFSUntrackPatterns = func(ctx context.Context, patterns []string, verbose bool, dryRun bool) (string, error) {
		called = true
		if len(patterns) != 1 || patterns[0] != "*.bam" {
			t.Fatalf("unexpected patterns: %v", patterns)
		}
		if !verbose {
			t.Fatalf("expected verbose=true")
		}
		if !dryRun {
			t.Fatalf("expected dryRun=true")
		}
		return "untracking output\n", nil
	}

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--verbose", "--dry-run", "*.bam"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !called {
		t.Fatalf("expected untrack helper to be called")
	}
	if out.String() != "untracking output\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRunUntrack_RequiresAtLeastOnePattern(t *testing.T) {
	cmd := NewCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected an error for missing required pattern")
	}
	if !strings.Contains(err.Error(), "requires at least 1 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}
