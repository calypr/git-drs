package drstrack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackPatterns_WritesGitattributes(t *testing.T) {
	repo := t.TempDir()
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	out, err := TrackPatterns(context.Background(), []string{"*.bam", "data/**"}, false, false)
	if err != nil {
		t.Fatalf("TrackPatterns: %v", err)
	}
	if !strings.Contains(out, "Tracking \"*.bam\"") || !strings.Contains(out, "Tracking \"data/**\"") {
		t.Fatalf("unexpected output: %q", out)
	}

	b, err := os.ReadFile(filepath.Join(repo, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "*.bam filter=drs diff=drs merge=drs -text") {
		t.Fatalf("missing tracked pattern in .gitattributes: %q", got)
	}
	if !strings.Contains(got, "data/** filter=drs diff=drs merge=drs -text") {
		t.Fatalf("missing tracked pattern in .gitattributes: %q", got)
	}
}

func TestTrackPatterns_DryRunDoesNotWrite(t *testing.T) {
	repo := t.TempDir()
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	out, err := TrackPatterns(context.Background(), []string{"*.bam"}, false, true)
	if err != nil {
		t.Fatalf("TrackPatterns: %v", err)
	}
	if !strings.Contains(out, "Tracking \"*.bam\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".gitattributes")); !os.IsNotExist(err) {
		t.Fatalf("expected no .gitattributes write in dry-run, stat err=%v", err)
	}
}

func TestListTrackedPatterns_ReadsGitattributes(t *testing.T) {
	repo := t.TempDir()
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	content := "*.bam filter=drs diff=drs merge=drs -text\n*.vcf filter=drs diff=drs merge=drs -text\n"
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	out, err := ListTrackedPatterns(context.Background(), true)
	if err != nil {
		t.Fatalf("ListTrackedPatterns: %v", err)
	}
	if !strings.Contains(out, "Listing tracked patterns") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "*.bam (.gitattributes)") || !strings.Contains(out, "*.vcf (.gitattributes)") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestUntrackPatterns_RemovesPattern(t *testing.T) {
	repo := t.TempDir()
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	content := "*.bam filter=drs diff=drs merge=drs -text\n*.vcf filter=drs diff=drs merge=drs -text\n"
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	out, err := UntrackPatterns(context.Background(), []string{"*.bam"}, false, false)
	if err != nil {
		t.Fatalf("UntrackPatterns: %v", err)
	}
	if !strings.Contains(out, "Untracking \"*.bam\"") {
		t.Fatalf("unexpected output: %q", out)
	}

	b, err := os.ReadFile(filepath.Join(repo, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	got := string(b)
	if strings.Contains(got, "*.bam filter=drs") {
		t.Fatalf("expected *.bam to be removed, got %q", got)
	}
	if !strings.Contains(got, "*.vcf filter=drs") {
		t.Fatalf("expected *.vcf to remain, got %q", got)
	}
}

func TestUntrackPatterns_DryRunDoesNotWrite(t *testing.T) {
	repo := t.TempDir()
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	content := "*.bam filter=drs diff=drs merge=drs -text\n"
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	out, err := UntrackPatterns(context.Background(), []string{"*.bam"}, false, true)
	if err != nil {
		t.Fatalf("UntrackPatterns: %v", err)
	}
	if !strings.Contains(out, "Untracking \"*.bam\"") {
		t.Fatalf("unexpected output: %q", out)
	}

	b, err := os.ReadFile(filepath.Join(repo, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	if string(b) != content {
		t.Fatalf("expected .gitattributes unchanged in dry-run, got %q", string(b))
	}
}

func mustChdirTrackTest(t *testing.T, dir string) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	return old
}
