package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackPatternsWritesGitattributes(t *testing.T) {
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

func TestTrackPatternsDryRunDoesNotWrite(t *testing.T) {
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

func TestTrackReadOnlyWritesRootAttributesFromSubdirectory(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	subdir := filepath.Join(repo, "nested")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldwd := mustChdirTrackTest(t, subdir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	const path = "data/pointer"
	if _, err := TrackReadOnly(context.Background(), path); err != nil {
		t.Fatalf("TrackReadOnly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(subdir, ".gitattributes")); !os.IsNotExist(err) {
		t.Fatalf("expected no nested .gitattributes, stat err=%v", err)
	}
	contents, err := os.ReadFile(filepath.Join(repo, ".gitattributes"))
	if err != nil {
		t.Fatalf("read root .gitattributes: %v", err)
	}
	got := string(contents)
	if !strings.Contains(got, "data/pointer filter=drs diff=drs merge=drs -text") {
		t.Fatalf("root .gitattributes does not track pointer: %q", got)
	}
	if !strings.Contains(got, "data/pointer drs=ro") {
		t.Fatalf("root .gitattributes does not mark pointer read-only: %q", got)
	}
}

func TestListTrackedPatternsReadsGitattributes(t *testing.T) {
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

func TestUntrackPatternsRemovesPattern(t *testing.T) {
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

func TestUntrackPatternsDryRunDoesNotWrite(t *testing.T) {
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
