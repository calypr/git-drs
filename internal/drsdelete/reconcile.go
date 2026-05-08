package drsdelete

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drsremote"
	sycommon "github.com/calypr/syfon/common"
)

type RefUpdate struct {
	OldSHA string
	NewSHA string
}

type Summary struct {
	DeletedRecords   int
	RemovedResources int
	ClearedLocalOnly int
	PendingMissing   int
	PendingAmbiguous int
}

func ReconcileCommittedDeletes(ctx context.Context, drsCtx *config.GitContext, refs []RefUpdate, logger *slog.Logger) (Summary, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return Summary{}, fmt.Errorf("DRS client unavailable")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if len(refs) == 0 {
		return Summary{}, nil
	}

	deletedByOID, err := collectDeletedPointers(ctx, refs)
	if err != nil {
		return Summary{}, err
	}
	if len(deletedByOID) == 0 {
		return Summary{}, nil
	}

	liveByOID, err := collectLivePathsByOID(refs, logger)
	if err != nil {
		return Summary{}, err
	}

	resource, err := sycommon.ResourcePath(drsCtx.Organization, drsCtx.ProjectId)
	if err != nil {
		return Summary{}, err
	}

	summary := Summary{}
	for oid, deletions := range deletedByOID {
		if livePaths := liveByOID[oid]; len(livePaths) > 0 {
			summary.ClearedLocalOnly += len(deletions)
			continue
		}

		records, err := drsremote.ObjectsByHashForScope(ctx, drsCtx, oid)
		if err != nil {
			return summary, err
		}
		switch len(records) {
		case 0:
			summary.PendingMissing += len(deletions)
			if logger != nil {
				logger.Warn("deleted pointer has no scoped DRS match", "oid", oid, "paths", deletedPaths(deletions))
			}
			continue
		case 1:
		default:
			summary.PendingAmbiguous += len(deletions)
			if logger != nil {
				logger.Warn("deleted pointer matched multiple scoped DRS records", "oid", oid, "count", len(records), "paths", deletedPaths(deletions))
			}
			continue
		}

		record := records[0]
		controlled := []string(nil)
		if record.ControlledAccess != nil {
			controlled = sycommon.NormalizeAccessResources(*record.ControlledAccess)
		}
		if len(controlled) <= 1 {
			var out map[string]any
			if err := drsCtx.Client.Requestor().Do(ctx, "DELETE", "/ga4gh/drs/v1/objects/"+record.Id, map[string]bool{
				"delete_object_metadata": true,
				"delete_storage_data":    true,
			}, &out); err != nil {
				return summary, err
			}
			summary.DeletedRecords++
			continue
		}

		var out map[string]any
		if err := drsCtx.Client.Requestor().Do(ctx, "POST", "/index/"+record.Id+"/controlled-access/remove", map[string]string{
			"resource": resource,
		}, &out); err != nil {
			return summary, err
		}
		summary.RemovedResources++
	}

	if logger != nil && (summary.DeletedRecords > 0 || summary.RemovedResources > 0 || summary.ClearedLocalOnly > 0 || summary.PendingMissing > 0 || summary.PendingAmbiguous > 0) {
		logger.Info("delete reconciliation complete",
			"deleted_records", summary.DeletedRecords,
			"removed_resources", summary.RemovedResources,
			"cleared_local_only", summary.ClearedLocalOnly,
			"pending_missing", summary.PendingMissing,
			"pending_ambiguous", summary.PendingAmbiguous,
		)
	}
	return summary, nil
}
