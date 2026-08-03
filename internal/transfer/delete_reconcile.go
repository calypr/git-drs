package transfer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	sycommon "github.com/calypr/syfon/common"
)

type RefUpdate struct {
	OldSHA string
	NewSHA string
}

type DeleteSummary struct {
	DeletedRecords   int
	RemovedResources int
	ClearedLocalOnly int
	PendingMissing   int
	PendingAmbiguous int
}

func ReconcileCommittedDeletes(ctx context.Context, drsCtx *remoteruntime.GitContext, refs []RefUpdate, logger *slog.Logger) (DeleteSummary, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return DeleteSummary{}, fmt.Errorf("DRS client unavailable")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if len(refs) == 0 {
		return DeleteSummary{}, nil
	}

	deletedByOID, err := collectDeletedPointers(ctx, refs)
	if err != nil {
		return DeleteSummary{}, err
	}
	if len(deletedByOID) == 0 {
		return DeleteSummary{}, nil
	}

	liveByOID, err := collectLivePathsByOID(refs, logger)
	if err != nil {
		return DeleteSummary{}, err
	}

	resource, err := sycommon.ResourcePath(drsCtx.Organization, drsCtx.ProjectId)
	if err != nil {
		return DeleteSummary{}, err
	}

	summary := DeleteSummary{}
	for oid, deletions := range deletedByOID {
		if livePaths := liveByOID[oid]; len(livePaths) > 0 {
			summary.ClearedLocalOnly += len(deletions)
			continue
		}

		records, err := lookup.ObjectsByHashForScope(ctx, drsCtx, oid)
		if err != nil {
			return summary, err
		}
		switch len(records) {
		case 0:
			summary.PendingMissing += len(deletions)
			logger.Warn("deleted pointer has no scoped DRS match", "oid", oid, "paths", deletedPaths(deletions))
			continue
		case 1:
		default:
			summary.PendingAmbiguous += len(deletions)
			logger.Warn("deleted pointer matched multiple scoped DRS records", "oid", oid, "count", len(records), "paths", deletedPaths(deletions))
			continue
		}

		record := records[0]
		controlled := []string(nil)
		if record.ControlledAccess != nil {
			controlled = sycommon.NormalizeAccessResources(*record.ControlledAccess)
		}
		if len(controlled) <= 1 {
			if err := drsCtx.Client.DRS().DeleteObject(ctx, record.Id, true); err != nil {
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

	if summary.DeletedRecords > 0 || summary.RemovedResources > 0 || summary.ClearedLocalOnly > 0 || summary.PendingMissing > 0 || summary.PendingAmbiguous > 0 {
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

func collectLivePathsByOID(refs []RefUpdate, logger *slog.Logger) (map[string][]string, error) {
	targets := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		newSHA := strings.TrimSpace(ref.NewSHA)
		if newSHA == "" || isZeroSHA(newSHA) {
			continue
		}
		if _, ok := seen[newSHA]; ok {
			continue
		}
		seen[newSHA] = struct{}{}
		targets = append(targets, newSHA)
	}
	if len(targets) == 0 {
		return map[string][]string{}, nil
	}

	files, err := lfs.GetLfsFilesForRefs(targets, logger)
	if err != nil {
		return nil, err
	}
	liveByOID := make(map[string][]string)
	for path, info := range files {
		oid := "sha256:" + strings.TrimPrefix(strings.TrimSpace(info.Oid), "sha256:")
		liveByOID[oid] = append(liveByOID[oid], path)
	}
	for oid := range liveByOID {
		sort.Strings(liveByOID[oid])
	}
	return liveByOID, nil
}
