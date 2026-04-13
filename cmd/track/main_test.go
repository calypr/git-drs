package track

import (
	"bytes"
	"context"
	"testing"
)

func TestRunTrack_TrackPatternsWithFlags(t *testing.T) {
	origTrack := gitLFSTrackPatterns
	origList := gitLFSListPatterns
	t.Cleanup(func() {
		gitLFSTrackPatterns = origTrack
		gitLFSListPatterns = origList
	})

	called := false
	gitLFSTrackPatterns = func(ctx context.Context, patterns []string, verbose bool, dryRun bool) (string, error) {
		called = true
		if len(patterns) != 2 || patterns[0] != "*.bam" || patterns[1] != "data/**" {
			t.Fatalf("unexpected patterns: %v", patterns)
		}
		if !verbose {
			t.Fatalf("expected verbose=true")
		}
		if !dryRun {
			t.Fatalf("expected dryRun=true")
		}
		return "tracking output\n", nil
	}
	gitLFSListPatterns = func(ctx context.Context, verbose bool) (string, error) {
		t.Fatalf("list should not be called when patterns are provided")
		return "", nil
	}

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--verbose", "--dry-run", "*.bam", "data/**"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !called {
		t.Fatalf("expected track helper to be called")
	}
	if out.String() != "tracking output\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRunTrack_ListWhenNoArgs(t *testing.T) {
	origTrack := gitLFSTrackPatterns
	origList := gitLFSListPatterns
	t.Cleanup(func() {
		gitLFSTrackPatterns = origTrack
		gitLFSListPatterns = origList
	})

	gitLFSTrackPatterns = func(ctx context.Context, patterns []string, verbose bool, dryRun bool) (string, error) {
		t.Fatalf("track should not be called when no patterns are provided")
		return "", nil
	}
	gitLFSListPatterns = func(ctx context.Context, verbose bool) (string, error) {
		if !verbose {
			t.Fatalf("expected verbose=true")
		}
		return "listing output\n", nil
	}

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--verbose"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.String() != "listing output\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
}
