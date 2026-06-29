package pull

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
)

func resetPullFlagsForTest() {
	includePatterns = nil
	dryRun = false
}

func TestCollectPointerFilesFiltersAndSorts(t *testing.T) {
	resetPullFlagsForTest()

	inventory := map[string]lfs.LfsFileInfo{
		"data/b.bin": {Name: "data/b.bin", Oid: "bbbb", Size: 2},
		"data/a.bin": {Name: "data/a.bin", Oid: "aaaa", Size: 1},
		"misc/c.bin": {Name: "misc/c.bin", Oid: "cccc", Size: 3},
	}

	files := collectPointerFiles(inventory, []string{"data/**"})
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Name != "data/a.bin" || files[1].Name != "data/b.bin" {
		t.Fatalf("unexpected file order: %+v", files)
	}
}

func TestPullDryRunListsMatchingPaths(t *testing.T) {
	resetPullFlagsForTest()

	oldLoadCfg := loadCfg
	oldResolveRemote := resolveRemote
	oldNewRemoteClient := newRemoteClient
	oldInventory := loadWorktreeInventory
	t.Cleanup(func() {
		loadCfg = oldLoadCfg
		resolveRemote = oldResolveRemote
		newRemoteClient = oldNewRemoteClient
		loadWorktreeInventory = oldInventory
	})

	loadCfg = func() (*config.Config, error) { return &config.Config{}, nil }
	resolveRemote = func(cfg *config.Config, name string) (config.Remote, error) { return config.Remote("origin"), nil }
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error) {
		t.Fatal("newRemoteClient should not be called during dry-run")
		return nil, nil
	}
	loadWorktreeInventory = func(_ *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			"data/a.bin": {Name: "data/a.bin", Oid: "aaaa", Size: 1},
			"misc/b.bin": {Name: "misc/b.bin", Oid: "bbbb", Size: 2},
		}, nil
	}

	includePatterns = []string{"data/**"}
	dryRun = true

	var out bytes.Buffer
	Cmd.SetOut(&out)
	Cmd.SetErr(&out)
	Cmd.SetArgs([]string{"--dry-run"})
	t.Cleanup(func() {
		Cmd.SetOut(nil)
		Cmd.SetErr(nil)
		Cmd.SetArgs(nil)
		resetPullFlagsForTest()
	})

	if err := Cmd.RunE(Cmd, []string{}); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if got := out.String(); got != "data/a.bin\n" {
		t.Fatalf("unexpected dry-run output: %q", got)
	}
}

func TestPullUsesTrackedInventoryForHydratedFiles(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")
	runGitCmdTest(t, repo, "config", "filter.drs.clean", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.smudge", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.process", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.required", "false")

	attrPath := filepath.Join(repo, ".gitattributes")
	if err := os.WriteFile(attrPath, []byte("*.dat filter=drs diff=drs merge=drs -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	oid := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	pointerPath := filepath.Join(repo, "data", "hydrated.dat")
	writePointerFile(t, pointerPath, oid, "321")

	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "commit tracked pointer")

	if err := os.WriteFile(pointerPath, []byte("localized payload"), 0o644); err != nil {
		t.Fatalf("hydrate tracked file: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	files, err := loadWorktreeInventory(drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("loadWorktreeInventory error: %v", err)
	}

	info, ok := files["data/hydrated.dat"]
	if !ok {
		t.Fatal("expected hydrated tracked file in pull inventory")
	}
	if info.Oid != oid || info.Size != 321 {
		t.Fatalf("unexpected hydrated tracked info: %+v", info)
	}
}

func TestInspectCachedObject(t *testing.T) {
	tmpDir := t.TempDir()
	objectPath := filepath.Join(tmpDir, "obj")

	state, err := inspectCachedObject(objectPath, "abc", 10)
	if err != nil {
		t.Fatalf("missing file returned error: %v", err)
	}
	if state.exists || state.complete {
		t.Fatal("missing file should not be complete")
	}

	if err := os.WriteFile(objectPath, []byte("12345"), 0o644); err != nil {
		t.Fatalf("write partial object: %v", err)
	}
	state, err = inspectCachedObject(objectPath, "abc", 10)
	if err != nil {
		t.Fatalf("partial file returned error: %v", err)
	}
	if !state.exists {
		t.Fatal("partial file should exist")
	}
	if state.complete {
		t.Fatal("partial file should not be complete")
	}

	fullContent := []byte("1234567890")
	sum := sha256.Sum256(fullContent)
	oid := hex.EncodeToString(sum[:])
	if err := os.WriteFile(objectPath, fullContent, 0o644); err != nil {
		t.Fatalf("write full object: %v", err)
	}
	state, err = inspectCachedObject(objectPath, oid, 10)
	if err != nil {
		t.Fatalf("complete file returned error: %v", err)
	}
	if !state.complete {
		t.Fatal("full file should be complete")
	}

	if err := os.WriteFile(objectPath, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatalf("write same-size corrupt object: %v", err)
	}
	state, err = inspectCachedObject(objectPath, oid, 10)
	if err != nil {
		t.Fatalf("same-size corrupt file returned error: %v", err)
	}
	if state.complete {
		t.Fatal("same-size corrupt file should not be complete")
	}
}

func writePointerFile(t *testing.T, path, oid, size string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir pointer dir: %v", err)
	}
	content := "version https://git-lfs.github.com/spec/v1\n" +
		"oid sha256:" + oid + "\n" +
		"size " + size + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write pointer file: %v", err)
	}
}

func runGitCmdTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
