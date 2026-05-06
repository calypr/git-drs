package pull

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/lfs"
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
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*config.GitContext, error) {
		return &config.GitContext{}, nil
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
