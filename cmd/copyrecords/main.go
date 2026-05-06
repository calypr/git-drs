package copyrecords

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	internalapi "github.com/calypr/syfon/apigen/client/internalapi"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syservices "github.com/calypr/syfon/client/services"
	"github.com/spf13/cobra"
)

var (
	batchSize int
)

type copyStats struct {
	SourceSeen int
	Created    int
	Updated    int
	Unchanged  int
	Written    int
}

type indexAPI interface {
	List(ctx context.Context, opts syservices.ListRecordsOptions) (internalapi.ListRecordsResponse, error)
	BulkDocuments(ctx context.Context, dids []string) ([]internalapi.InternalRecordResponse, error)
	CreateBulk(ctx context.Context, req internalapi.BulkCreateRequest) (internalapi.ListRecordsResponse, error)
}

var Cmd = &cobra.Command{
	Use:   "copy-records [source-remote] <target-remote> <organization/project>",
	Short: "Copy Syfon records between remotes for one organization/project scope",
	Long:  "Read all Syfon records for a source organization/project scope and bulk load them into a target Syfon instance, only merging controlled_access and access_methods for records that already exist on the target.",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %w", err)
		}

		sourceRemote := ""
		targetRemote := ""
		scopeArg := ""
		if len(args) == 2 {
			targetRemote = args[0]
			scopeArg = args[1]
		} else {
			sourceRemote = args[0]
			targetRemote = args[1]
			scopeArg = args[2]
		}

		srcRemoteName, err := cfg.GetRemoteOrDefault(sourceRemote)
		if err != nil {
			return fmt.Errorf("error resolving source remote: %w", err)
		}
		if strings.TrimSpace(targetRemote) == "" {
			return fmt.Errorf("target remote is required")
		}
		dstRemoteName := config.Remote(targetRemote)
		if srcRemoteName == dstRemoteName {
			return fmt.Errorf("source and target remotes must be different")
		}

		srcCfg := cfg.GetRemote(srcRemoteName)
		if srcCfg == nil {
			return fmt.Errorf("source remote %q not found", srcRemoteName)
		}

		org, proj, err := parseScopeArg(scopeArg)
		if err != nil {
			return err
		}

		srcCtx, err := cfg.GetRemoteClient(srcRemoteName, logger)
		if err != nil {
			return fmt.Errorf("error creating source client: %w", err)
		}
		dstCtx, err := cfg.GetRemoteClient(dstRemoteName, logger)
		if err != nil {
			return fmt.Errorf("error creating target client: %w", err)
		}

		stats, err := copyProjectRecords(cmd.Context(), logger, srcCtx.Client.Index(), dstCtx.Client.Index(), org, proj, batchSize)
		if err != nil {
			return err
		}

		logger.Info("copy-records complete",
			"source_remote", srcRemoteName,
			"target_remote", dstRemoteName,
			"organization", org,
			"project", proj,
			"source_seen", stats.SourceSeen,
			"created", stats.Created,
			"updated", stats.Updated,
			"unchanged", stats.Unchanged,
			"written", stats.Written,
		)
		return nil
	},
}

func init() {
	Cmd.Flags().IntVar(&batchSize, "batch-size", 250, "records per source page and target bulk write")
}

func parseScopeArg(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("scope is required and must be in organization/project form")
	}
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid scope %q: expected organization/project", raw)
	}
	org := strings.TrimSpace(parts[0])
	project := strings.TrimSpace(parts[1])
	if org == "" || project == "" {
		return "", "", fmt.Errorf("invalid scope %q: expected organization/project", raw)
	}
	return org, project, nil
}

func copyProjectRecords(ctx context.Context, logger *slog.Logger, src indexAPI, dst indexAPI, org, project string, batchSize int) (copyStats, error) {
	if batchSize <= 0 {
		batchSize = 250
	}

	stats := copyStats{}
	page := 1
	for {
		listResp, err := src.List(ctx, syservices.ListRecordsOptions{
			Organization: org,
			ProjectID:    project,
			Limit:        batchSize,
			Page:         page,
		})
		if err != nil {
			return stats, fmt.Errorf("source list failed for %s/%s page %d: %w", org, project, page, err)
		}
		records := []internalapi.InternalRecord{}
		if listResp.Records != nil {
			records = *listResp.Records
		}
		if len(records) == 0 {
			break
		}
		stats.SourceSeen += len(records)

		toWrite, batchStats, err := buildMergedBatch(ctx, dst, records)
		if err != nil {
			return stats, err
		}
		stats.Created += batchStats.Created
		stats.Updated += batchStats.Updated
		stats.Unchanged += batchStats.Unchanged

		if len(toWrite) > 0 {
			resp, err := dst.CreateBulk(ctx, internalapi.BulkCreateRequest{Records: toWrite})
			if err != nil {
				return stats, fmt.Errorf("target bulk create failed on page %d: %w", page, err)
			}
			if resp.Records != nil {
				stats.Written += len(*resp.Records)
			} else {
				stats.Written += len(toWrite)
			}
		}

		if logger != nil {
			logger.Info("copy-records batch complete",
				"organization", org,
				"project", project,
				"page", page,
				"source_records", len(records),
				"created", batchStats.Created,
				"updated", batchStats.Updated,
				"unchanged", batchStats.Unchanged,
				"written", len(toWrite),
			)
		}

		if len(records) < batchSize {
			break
		}
		page++
	}

	return stats, nil
}

func buildMergedBatch(ctx context.Context, dst indexAPI, source []internalapi.InternalRecord) ([]internalapi.InternalRecord, copyStats, error) {
	stats := copyStats{}
	if len(source) == 0 {
		return nil, stats, nil
	}

	dids := make([]string, 0, len(source))
	for _, rec := range source {
		did := strings.TrimSpace(rec.Did)
		if did == "" {
			continue
		}
		dids = append(dids, did)
	}

	existing, err := dst.BulkDocuments(ctx, dids)
	if err != nil {
		return nil, stats, fmt.Errorf("target bulk documents failed: %w", err)
	}
	existingByDID := make(map[string]internalapi.InternalRecord, len(existing))
	for _, rec := range existing {
		existingByDID[strings.TrimSpace(rec.Did)] = recordResponseToRecord(rec)
	}

	out := make([]internalapi.InternalRecord, 0, len(source))
	for _, src := range source {
		did := strings.TrimSpace(src.Did)
		if did == "" {
			continue
		}
		if dstRec, ok := existingByDID[did]; ok {
			merged, changed := mergeExistingRecord(dstRec, src)
			if changed {
				out = append(out, merged)
				stats.Updated++
			} else {
				stats.Unchanged++
			}
			continue
		}
		out = append(out, src)
		stats.Created++
	}

	return out, stats, nil
}

func mergeExistingRecord(dst, src internalapi.InternalRecord) (internalapi.InternalRecord, bool) {
	merged := dst
	changed := false

	controlledAccess := mergeStringLists(dst.ControlledAccess, src.ControlledAccess)
	if !equalStringPointers(merged.ControlledAccess, controlledAccess) {
		merged.ControlledAccess = controlledAccess
		changed = true
	}

	accessMethods := mergeAccessMethods(dst.AccessMethods, src.AccessMethods)
	if !equalAccessMethodPointers(merged.AccessMethods, accessMethods) {
		merged.AccessMethods = accessMethods
		changed = true
	}

	return merged, changed
}

func recordResponseToRecord(in internalapi.InternalRecordResponse) internalapi.InternalRecord {
	return internalapi.InternalRecord{
		Did:              in.Did,
		AccessMethods:    in.AccessMethods,
		ControlledAccess: in.ControlledAccess,
		CreatedTime:      in.CreatedTime,
		Description:      in.Description,
		FileName:         in.FileName,
		Hashes:           in.Hashes,
		Organization:     in.Organization,
		Project:          in.Project,
		Size:             in.Size,
		UpdatedTime:      in.UpdatedTime,
		Version:          in.Version,
	}
}

func mergeStringLists(left, right *[]string) *[]string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, list := range []*[]string{left, right} {
		if list == nil {
			continue
		}
		for _, raw := range *list {
			val := strings.TrimSpace(raw)
			if val == "" {
				continue
			}
			if _, ok := seen[val]; ok {
				continue
			}
			seen[val] = struct{}{}
			out = append(out, val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func mergeAccessMethods(left, right *[]drsapi.AccessMethod) *[]drsapi.AccessMethod {
	seen := map[string]struct{}{}
	out := make([]drsapi.AccessMethod, 0)
	for _, list := range []*[]drsapi.AccessMethod{left, right} {
		if list == nil {
			continue
		}
		for _, method := range *list {
			key := canonicalAccessMethod(method)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, method)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func canonicalAccessMethod(method drsapi.AccessMethod) string {
	b, err := json.Marshal(method)
	if err != nil {
		return fmt.Sprintf("%s|%v", method.Type, method.AccessId)
	}
	return string(b)
}

func equalStringPointers(a, b *[]string) bool {
	return equalJSON(a, b)
}

func equalAccessMethodPointers(a, b *[]drsapi.AccessMethod) bool {
	return equalJSON(a, b)
}

func equalJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
