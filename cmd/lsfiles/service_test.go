package lsfiles

import (
	"context"
	"log/slog"
	"testing"

	"github.com/calypr/git-drs/internal/lfs"
)

func TestCollectRowsMatchesDirectoryOperands(t *testing.T) {
	previous := loadLFSInventory
	loadLFSInventory = func(_, _ string, _ []string, _ *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			"UMB/adonis_dmello/Seurat_batch1_16um/4-R3_master.rds": {Oid: "nested", IsPointer: true},
			"UMBRELLA/other.rds": {Oid: "sibling", IsPointer: true},
		}, nil
	}
	t.Cleanup(func() { loadLFSInventory = previous })

	for _, operand := range []string{"UMB/", "UMB", "./UMB/"} {
		t.Run(operand, func(t *testing.T) {
			rows, err := collectRows(context.Background(), "", "", []string{operand}, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Path != "UMB/adonis_dmello/Seurat_batch1_16um/4-R3_master.rds" {
				t.Fatalf("rows for %q = %+v, want nested UMB file only", operand, rows)
			}
		})
	}
}
