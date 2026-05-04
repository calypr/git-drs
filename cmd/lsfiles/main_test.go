package lsfiles

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/spf13/cobra"
)

func TestCollectRowsAndPrintRows(t *testing.T) {
	oldLoadConfig := loadConfig
	oldResolveRemote := resolveRemote
	oldNewRemoteClient := newRemoteClient
	oldLoadLFSInventory := loadLFSInventory
	oldLookupScopedObjects := lookupScopedObjects
	t.Cleanup(func() {
		loadConfig = oldLoadConfig
		resolveRemote = oldResolveRemote
		newRemoteClient = oldNewRemoteClient
		loadLFSInventory = oldLoadLFSInventory
		lookupScopedObjects = oldLookupScopedObjects
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
			"b/file2.bin": {Name: "b/file2.bin", Oid: strings.Repeat("b", 64)},
			"a/file1.bin": {Name: "a/file1.bin", Oid: strings.Repeat("a", 64)},
			"c/file3.bin": {Name: "c/file3.bin", Oid: strings.Repeat("c", 64)},
		}, nil
	}
	lookupScopedObjects = func(ctx context.Context, drsCtx *config.GitContext, checksum string) ([]drsapi.DrsObject, error) {
		switch checksum {
		case strings.Repeat("a", 64):
			return []drsapi.DrsObject{{Id: "did-1"}}, nil
		case strings.Repeat("b", 64):
			return nil, nil
		default:
			return nil, errors.New("lookup failed")
		}
	}

	cmd := &cobra.Command{}
	rows, err := collectRows(cmd, "", "")
	if err != nil {
		t.Fatalf("collectRows returned error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Path != "a/file1.bin" || rows[0].Status != "present" || rows[0].Detail != "drs://did-1" {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].Path != "b/file2.bin" || rows[1].Status != "missing" || rows[1].Detail != "-" {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
	if rows[2].Path != "c/file3.bin" || rows[2].Status != "error" || rows[2].Detail != "lookup failed" {
		t.Fatalf("unexpected third row: %+v", rows[2])
	}

	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := printRows(cmd, rows); err != nil {
		t.Fatalf("printRows returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "OID\tSTATUS\tPATH\tDETAIL\n") {
		t.Fatalf("missing header in output: %q", got)
	}
	if !strings.Contains(got, rows[0].OID+"\tpresent\ta/file1.bin\tdrs://did-1\n") {
		t.Fatalf("missing present row: %q", got)
	}
	if !strings.Contains(got, rows[1].OID+"\tmissing\tb/file2.bin\t-\n") {
		t.Fatalf("missing missing row: %q", got)
	}
	if !strings.Contains(got, rows[2].OID+"\terror\tc/file3.bin\tlookup failed\n") {
		t.Fatalf("missing error row: %q", got)
	}
}
