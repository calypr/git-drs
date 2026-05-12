package lsfiles

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/spf13/cobra"
)

func resetFlagsForTest() {
	gitRemote = ""
	drsRemote = ""
	includePatterns = nil
	showLong = false
	nameOnly = false
	jsonOutput = false
	drsStatus = false
}

func TestCollectRowsLocalDefault(t *testing.T) {
	resetFlagsForTest()

	oldLoadLFSInventory := loadLFSInventory
	oldLookupScopedObjectsBatch := lookupScopedObjectsBatch
	oldResolveDefaultRemote := resolveDefaultRemote
	t.Cleanup(func() {
		loadLFSInventory = oldLoadLFSInventory
		lookupScopedObjectsBatch = oldLookupScopedObjectsBatch
		resolveDefaultRemote = oldResolveDefaultRemote
	})

	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tempdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	localizedPath := filepath.Join("a", "localized.bin")
	pointerPath := filepath.Join("b", "pointer.bin")
	if err := os.MkdirAll(filepath.Dir(localizedPath), 0o755); err != nil {
		t.Fatalf("mkdir localized dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(pointerPath), 0o755); err != nil {
		t.Fatalf("mkdir pointer dir: %v", err)
	}
	if err := os.WriteFile(localizedPath, []byte("hydrated-bytes"), 0o644); err != nil {
		t.Fatalf("write localized file: %v", err)
	}
	pointerContent := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("b", 64) + "\nsize 12\n"
	if err := os.WriteFile(pointerPath, []byte(pointerContent), 0o644); err != nil {
		t.Fatalf("write pointer file: %v", err)
	}

	loadLFSInventory = func(gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			localizedPath: {Name: localizedPath, Oid: strings.Repeat("a", 64)},
			pointerPath:   {Name: pointerPath, Oid: strings.Repeat("b", 64)},
		}, nil
	}
	lookupScopedObjectsBatch = func(ctx context.Context, drsCtx *config.GitContext, checksums []string) (map[string][]drsapi.DrsObject, error) {
		t.Fatalf("unexpected remote lookup for checksums %v", checksums)
		return nil, nil
	}
	resolveDefaultRemote = func() string { return "" }

	cmd := &cobra.Command{}
	rows, err := collectRows(cmd, "", "", nil, false)
	if err != nil {
		t.Fatalf("collectRows returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Path != localizedPath || rows[0].Status != "*" || !rows[0].Localized {
		t.Fatalf("unexpected localized row: %+v", rows[0])
	}
	if rows[1].Path != pointerPath || rows[1].Status != "-" || rows[1].Localized {
		t.Fatalf("unexpected pointer row: %+v", rows[1])
	}

	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := printRows(cmd, rows); err != nil {
		t.Fatalf("printRows returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, rows[0].ShortOID+" * "+localizedPath+"\n") {
		t.Fatalf("missing localized row: %q", got)
	}
	if !strings.Contains(got, rows[1].ShortOID+" - "+pointerPath+"\n") {
		t.Fatalf("missing pointer row: %q", got)
	}
}

func TestCollectRowsWithDRSLookupAndFilters(t *testing.T) {
	resetFlagsForTest()

	oldLoadConfig := loadConfig
	oldResolveRemote := resolveRemote
	oldNewRemoteClient := newRemoteClient
	oldLoadLFSInventory := loadLFSInventory
	oldListRemoteRefs := listRemoteRefs
	oldLookupScopedObjectsBatch := lookupScopedObjectsBatch
	oldResolveDefaultRemote := resolveDefaultRemote
	t.Cleanup(func() {
		loadConfig = oldLoadConfig
		resolveRemote = oldResolveRemote
		newRemoteClient = oldNewRemoteClient
		loadLFSInventory = oldLoadLFSInventory
		listRemoteRefs = oldListRemoteRefs
		lookupScopedObjectsBatch = oldLookupScopedObjectsBatch
		resolveDefaultRemote = oldResolveDefaultRemote
	})

	loadConfig = func() (*config.Config, error) {
		return &config.Config{}, nil
	}
	resolveRemote = func(cfg *config.Config, name string) (config.Remote, error) {
		return config.Remote("origin"), nil
	}
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*config.GitContext, error) {
		return &config.GitContext{}, nil
	}

	loadLFSInventory = func(gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			"a/file1.bin":    {Name: "a/file1.bin", Oid: strings.Repeat("a", 64)},
			"data/file2.bam": {Name: "data/file2.bam", Oid: strings.Repeat("b", 64)},
			"data/file3.txt": {Name: "data/file3.txt", Oid: strings.Repeat("c", 64)},
		}, nil
	}
	listRemoteRefs = func(remote string) ([]string, error) {
		if remote == "" {
			return nil, nil
		}
		return []string{"refs/remotes/dev/main"}, nil
	}
	lookupScopedObjectsBatch = func(ctx context.Context, drsCtx *config.GitContext, checksums []string) (map[string][]drsapi.DrsObject, error) {
		got := map[string][]drsapi.DrsObject{}
		for _, checksum := range checksums {
			switch checksum {
			case strings.Repeat("b", 64):
				got[checksum] = []drsapi.DrsObject{{Id: "did-1"}}
			default:
				got[checksum] = nil
			}
		}
		return got, nil
	}
	resolveDefaultRemote = func() string { return "" }

	cmd := &cobra.Command{}
	rows, err := collectRows(cmd, "dev", "", []string{"data/**"}, true)
	if err != nil {
		t.Fatalf("collectRows returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Path != "data/file2.bam" || !rows[0].Registered || rows[0].Detail != "drs://did-1" {
		t.Fatalf("unexpected registered row: %+v", rows[0])
	}
	if rows[1].Path != "data/file3.txt" || rows[1].Registered || rows[1].Detail != "" {
		t.Fatalf("unexpected unregistered row: %+v", rows[1])
	}

	drsStatus = true
	showLong = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := printRows(cmd, rows); err != nil {
		t.Fatalf("printRows returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, rows[0].OID+" - data/file2.bam\tdrs://did-1\n") {
		t.Fatalf("missing registered row: %q", got)
	}
	if !strings.Contains(got, rows[1].OID+" - data/file3.txt\t-\n") {
		t.Fatalf("missing unregistered row: %q", got)
	}
}

func TestCollectRowsWithDRSLookupBatchError(t *testing.T) {
	resetFlagsForTest()

	oldLoadConfig := loadConfig
	oldResolveRemote := resolveRemote
	oldNewRemoteClient := newRemoteClient
	oldLoadLFSInventory := loadLFSInventory
	oldListRemoteRefs := listRemoteRefs
	oldLookupScopedObjectsBatch := lookupScopedObjectsBatch
	oldResolveDefaultRemote := resolveDefaultRemote
	t.Cleanup(func() {
		loadConfig = oldLoadConfig
		resolveRemote = oldResolveRemote
		newRemoteClient = oldNewRemoteClient
		loadLFSInventory = oldLoadLFSInventory
		listRemoteRefs = oldListRemoteRefs
		lookupScopedObjectsBatch = oldLookupScopedObjectsBatch
		resolveDefaultRemote = oldResolveDefaultRemote
	})

	loadConfig = func() (*config.Config, error) { return &config.Config{}, nil }
	resolveRemote = func(cfg *config.Config, name string) (config.Remote, error) {
		return config.Remote("origin"), nil
	}
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*config.GitContext, error) {
		return &config.GitContext{}, nil
	}
	loadLFSInventory = func(gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			"data/file2.bam": {Name: "data/file2.bam", Oid: strings.Repeat("b", 64)},
			"data/file3.txt": {Name: "data/file3.txt", Oid: strings.Repeat("c", 64)},
		}, nil
	}
	listRemoteRefs = func(remote string) ([]string, error) {
		if remote == "" {
			return nil, nil
		}
		return []string{"refs/remotes/dev/main"}, nil
	}
	lookupScopedObjectsBatch = func(ctx context.Context, drsCtx *config.GitContext, checksums []string) (map[string][]drsapi.DrsObject, error) {
		return nil, errors.New("lookup failed")
	}
	resolveDefaultRemote = func() string { return "" }

	cmd := &cobra.Command{}
	rows, err := collectRows(cmd, "dev", "", []string{"data/**"}, true)
	if err != nil {
		t.Fatalf("collectRows returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	for _, row := range rows {
		if row.Detail != "lookup failed" {
			t.Fatalf("expected shared batch lookup error, got row=%+v", row)
		}
	}
}

func TestCollectRowsUsesRemoteRefsWhenGitRemoteProvided(t *testing.T) {
	resetFlagsForTest()

	oldLoadLFSInventory := loadLFSInventory
	oldListRemoteRefs := listRemoteRefs
	oldResolveDefaultRemote := resolveDefaultRemote
	t.Cleanup(func() {
		loadLFSInventory = oldLoadLFSInventory
		listRemoteRefs = oldListRemoteRefs
		resolveDefaultRemote = oldResolveDefaultRemote
	})

	listRemoteRefs = func(remote string) ([]string, error) {
		if remote != "dev" {
			t.Fatalf("unexpected remote %q", remote)
		}
		return []string{"refs/remotes/dev/main", "refs/remotes/dev/release"}, nil
	}

	loadLFSInventory = func(gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		if gitRemoteName != "dev" {
			t.Fatalf("unexpected git remote name %q", gitRemoteName)
		}
		if len(branches) != 2 || branches[0] != "refs/remotes/dev/main" || branches[1] != "refs/remotes/dev/release" {
			t.Fatalf("unexpected refs %v", branches)
		}
		return map[string]lfs.LfsFileInfo{
			"data/file.bam": {Name: "data/file.bam", Oid: strings.Repeat("a", 64)},
		}, nil
	}
	resolveDefaultRemote = func() string {
		t.Fatal("default remote fallback should not be used when --git-remote is set")
		return ""
	}

	cmd := &cobra.Command{}
	rows, err := collectRows(cmd, "dev", "", nil, false)
	if err != nil {
		t.Fatalf("collectRows returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].Path != "data/file.bam" {
		t.Fatalf("unexpected rows %+v", rows)
	}
}

func TestCollectRowsFallsBackToDefaultRemoteWhenLocalInventoryEmpty(t *testing.T) {
	resetFlagsForTest()

	oldLoadLFSInventory := loadLFSInventory
	oldListRemoteRefs := listRemoteRefs
	oldResolveDefaultRemote := resolveDefaultRemote
	t.Cleanup(func() {
		loadLFSInventory = oldLoadLFSInventory
		listRemoteRefs = oldListRemoteRefs
		resolveDefaultRemote = oldResolveDefaultRemote
	})

	callCount := 0
	loadLFSInventory = func(gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		callCount++
		if callCount == 1 {
			if gitRemoteName != "" || len(branches) != 0 {
				t.Fatalf("first inventory call should be local-only, got remote=%q refs=%v", gitRemoteName, branches)
			}
			return map[string]lfs.LfsFileInfo{}, nil
		}
		if gitRemoteName != "dev" {
			t.Fatalf("expected fallback remote dev, got %q", gitRemoteName)
		}
		if len(branches) != 1 || branches[0] != "refs/remotes/dev/main" {
			t.Fatalf("unexpected fallback refs: %v", branches)
		}
		return map[string]lfs.LfsFileInfo{
			"data/file2.bam": {Name: "data/file2.bam", Oid: strings.Repeat("b", 64)},
		}, nil
	}
	resolveDefaultRemote = func() string { return "dev" }
	listRemoteRefs = func(remote string) ([]string, error) {
		if remote != "dev" {
			t.Fatalf("expected fallback remote query for dev, got %q", remote)
		}
		return []string{"refs/remotes/dev/main"}, nil
	}

	cmd := &cobra.Command{}
	rows, err := collectRows(cmd, "", "", nil, false)
	if err != nil {
		t.Fatalf("collectRows returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].Path != "data/file2.bam" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 inventory calls, got %d", callCount)
	}
}

func TestValidateOutputFlags(t *testing.T) {
	resetFlagsForTest()

	nameOnly = true
	jsonOutput = true
	if err := validateOutputFlags(); err == nil {
		t.Fatal("expected name-only/json conflict")
	}

	resetFlagsForTest()
	nameOnly = true
	showLong = true
	if err := validateOutputFlags(); err == nil {
		t.Fatal("expected long/name-only conflict")
	}
}
