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

func TestTrackReadOnlyMatchesLiteralSpecialNames(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	for _, path := range []string{"a[1].bin", "lead space.bin", "a [1].bin", "star*.bin", "!bang.bin"} {
		for i := 0; i < 2; i++ {
			if _, err := TrackReadOnly(context.Background(), path); err != nil {
				t.Fatalf("track %q: %v", path, err)
			}
		}
		cmd := exec.Command("git", "check-attr", "filter", "drs", "--", path)
		cmd.Dir = repo
		out, err := cmd.Output()
		if err != nil || !strings.Contains(string(out), path+": filter: drs") || !strings.Contains(string(out), path+": drs: ro") {
			t.Fatalf("attributes for %q: %q, %v", path, out, err)
		}
	}
	for _, path := range []string{"a1.bin", "a 1.bin", "starX.bin", "lead\tspace.bin"} {
		cmd := exec.Command("git", "check-attr", "filter", "drs", "--", path)
		cmd.Dir = repo
		out, err := cmd.Output()
		if err != nil || strings.Contains(string(out), "filter: drs") || strings.Contains(string(out), "drs: ro") {
			t.Fatalf("literal rule matched unrelated %q: %q, %v", path, out, err)
		}
	}
	attributes, err := os.ReadFile(filepath.Join(repo, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(attributes), "filter=drs"); got != 5 {
		t.Fatalf("repeated tracking wrote %d filter rules, want 5: %q", got, attributes)
	}
}

func TestTrackReadOnlyReplacesLegacyWhitespaceClass(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	legacy := "lead[[:space:]]space.bin filter=drs diff=drs merge=drs -text\nlead[[:space:]]space.bin drs=ro\n"
	if err := os.WriteFile(".gitattributes", []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TrackReadOnly(context.Background(), "lead space.bin"); err != nil {
		t.Fatal(err)
	}
	attributes, err := os.ReadFile(".gitattributes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(attributes), "[[:space:]]") || strings.Count(string(attributes), "filter=drs") != 1 {
		t.Fatalf("legacy broad rule remains: %q", attributes)
	}
	cmd = exec.Command("git", "check-attr", "filter", "drs", "--", "lead\tspace.bin")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil || strings.Contains(string(out), "filter: drs") || strings.Contains(string(out), "drs: ro") {
		t.Fatalf("legacy rule still matches tab sibling: %q, %v", out, err)
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

func TestUntrackPatternsMatchesTrackWithLeadingSlash(t *testing.T) {
	repo := t.TempDir()
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	if _, err := TrackPatterns(context.Background(), []string{"/data/*.bam"}, false, false); err != nil {
		t.Fatal(err)
	}
	out, err := UntrackPatterns(context.Background(), []string{"/data/*.bam"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `Untracking "data/*.bam"`) {
		t.Fatalf("expected tracked pattern to be removed, got %q", out)
	}
	attributes, err := os.ReadFile(filepath.Join(repo, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(attributes), "filter=drs") {
		t.Fatalf("tracked rule remains: %q", attributes)
	}
}

func TestListAndUntrackQuotedLiteralPatterns(t *testing.T) {
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	oldwd := mustChdirTrackTest(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	for _, path := range []string{"lead space.bin", "a[1].bin"} {
		if _, err := TrackReadOnly(context.Background(), path); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := ListTrackedPatterns(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed, "lead space.bin (.gitattributes)") {
		t.Fatalf("quoted path missing from listing: %q", listed)
	}
	out, err := UntrackPatterns(context.Background(), []string{"lead space.bin", "a[1].bin"}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `Untracking "lead space.bin"`) || !strings.Contains(out, `Untracking "a[1].bin"`) {
		t.Fatalf("literal paths were not untracked: %q", out)
	}
	attributes, err := os.ReadFile(filepath.Join(repo, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(attributes), "filter=drs") {
		t.Fatalf("filter rules remain after untracking: %q", attributes)
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
