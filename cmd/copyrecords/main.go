package copyrecords

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sycommon "github.com/calypr/syfon/common"
	"github.com/calypr/syfon/client/request"
	syservices "github.com/calypr/syfon/client/services"
	"github.com/spf13/cobra"
)

var (
	batchSize             int
	overwriteNameFileName bool
)

type copyStats struct {
	SourceSeen int
	Created    int
	Updated    int
	Unchanged  int
	Written    int
}

type copyHashInfo map[string]string

type copyRecord struct {
	AccessMethods    *[]drsapi.AccessMethod `json:"access_methods,omitempty"`
	ControlledAccess *[]string              `json:"controlled_access,omitempty"`
	CreatedTime      *string                `json:"created_time,omitempty"`
	Description      *string                `json:"description,omitempty"`
	Did              string                 `json:"did"`
	FileName         *string                `json:"file_name,omitempty"`
	Name             *string                `json:"name,omitempty"`
	Hashes           *copyHashInfo          `json:"hashes,omitempty"`
	Organization     *string                `json:"organization,omitempty"`
	Project          *string                `json:"project,omitempty"`
	Size             *int64                 `json:"size,omitempty"`
	UpdatedTime      *string                `json:"updated_time,omitempty"`
	Version          *string                `json:"version,omitempty"`
}

type copyListRecordsResponse struct {
	Records *[]copyRecord `json:"records,omitempty"`
}

type copyBulkCreateRequest struct {
	Records []copyRecord `json:"records"`
}

type indexAPI interface {
	List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error)
	BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error)
	CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error)
}

type rawIndexAPI struct {
	requestor request.Requester
}

func newRawIndexAPI(requestor request.Requester) *rawIndexAPI {
	return &rawIndexAPI{requestor: requestor}
}

func (r *rawIndexAPI) List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error) {
	params := url.Values{}
	if opts.Hash != "" {
		params.Set("hash", opts.Hash)
	}
	if opts.URL != "" {
		params.Set("url", opts.URL)
	}
	if opts.Organization != "" {
		params.Set("organization", opts.Organization)
	}
	if opts.ProjectID != "" {
		params.Set("project", opts.ProjectID)
	}
	if opts.Limit != 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Page != 0 {
		params.Set("page", fmt.Sprintf("%d", opts.Page))
	}
	var out copyListRecordsResponse
	if err := r.requestor.Do(ctx, http.MethodGet, "/index", nil, &out, request.WithQueryValues(params)); err != nil {
		return copyListRecordsResponse{}, err
	}
	return out, nil
}

func (r *rawIndexAPI) BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error) {
	var out []copyRecord
	if err := r.requestor.Do(ctx, http.MethodPost, "/index/bulk/documents", dids, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *rawIndexAPI) CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error) {
	var out copyListRecordsResponse
	if err := r.requestor.Do(ctx, http.MethodPost, "/index/bulk", req, &out); err != nil {
		return copyListRecordsResponse{}, err
	}
	return out, nil
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

		stats, err := copyProjectRecords(
			cmd.Context(),
			logger,
			newRawIndexAPI(srcCtx.Client.Requestor()),
			newRawIndexAPI(dstCtx.Client.Requestor()),
			org,
			proj,
			batchSize,
			overwriteNameFileName,
		)
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
	Cmd.Flags().BoolVar(&overwriteNameFileName, "overwrite-name-file-name", false, "for existing target records, replace target name and file_name with the source values")
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

func copyProjectRecords(ctx context.Context, logger *slog.Logger, src indexAPI, dst indexAPI, org, project string, batchSize int, overwriteNameFileName bool) (copyStats, error) {
	if batchSize <= 0 {
		batchSize = 250
	}

	stats := copyStats{}
	page := 1
	fallbackUsed := false
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
		records := []copyRecord{}
		if listResp.Records != nil {
			records = *listResp.Records
		}
		if page == 1 && len(records) == 0 {
			fallbackRecords, fallbackErr := listSourceRecordsByControlledAccess(ctx, src, org, project, batchSize)
			if fallbackErr != nil {
				return stats, fallbackErr
			}
			if len(fallbackRecords) > 0 {
				records = fallbackRecords
				fallbackUsed = true
			}
		}
		if len(records) == 0 {
			break
		}
		stats.SourceSeen += len(records)

		toWrite, batchStats, err := buildMergedBatch(ctx, dst, records, overwriteNameFileName)
		if err != nil {
			return stats, err
		}
		stats.Created += batchStats.Created
		stats.Updated += batchStats.Updated
		stats.Unchanged += batchStats.Unchanged

		if len(toWrite) > 0 {
			resp, err := dst.CreateBulk(ctx, copyBulkCreateRequest{Records: toWrite})
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
				"fallback_scope_scan", fallbackUsed,
				"source_records", len(records),
				"created", batchStats.Created,
				"updated", batchStats.Updated,
				"unchanged", batchStats.Unchanged,
				"written", len(toWrite),
			)
		}

		if fallbackUsed || len(records) < batchSize {
			break
		}
		page++
	}

	return stats, nil
}

func listSourceRecordsByControlledAccess(ctx context.Context, src indexAPI, org, project string, batchSize int) ([]copyRecord, error) {
	resource, err := sycommon.ResourcePath(org, project)
	if err != nil {
		return nil, fmt.Errorf("invalid scope %s/%s: %w", org, project, err)
	}
	if batchSize <= 0 {
		batchSize = 250
	}

	page := 1
	out := make([]copyRecord, 0)
	seen := map[string]struct{}{}
	for {
		listResp, err := src.List(ctx, syservices.ListRecordsOptions{
			Limit: batchSize,
			Page:  page,
		})
		if err != nil {
			return nil, fmt.Errorf("fallback source list failed for %s/%s page %d: %w", org, project, page, err)
		}
		records := []copyRecord{}
		if listResp.Records != nil {
			records = *listResp.Records
		}
		if len(records) == 0 {
			break
		}
		for _, rec := range records {
			if !recordHasControlledAccess(rec, resource) {
				continue
			}
			did := strings.TrimSpace(rec.Did)
			if did == "" {
				continue
			}
			if _, ok := seen[did]; ok {
				continue
			}
			seen[did] = struct{}{}
			out = append(out, rec)
		}
		if len(records) < batchSize {
			break
		}
		page++
	}
	return out, nil
}

func recordHasControlledAccess(rec copyRecord, resource string) bool {
	if rec.ControlledAccess == nil {
		return false
	}
	for _, candidate := range *rec.ControlledAccess {
		if strings.TrimSpace(candidate) == resource {
			return true
		}
	}
	return false
}

func buildMergedBatch(ctx context.Context, dst indexAPI, source []copyRecord, overwriteNameFileName bool) ([]copyRecord, copyStats, error) {
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
	existingByDID := make(map[string]copyRecord, len(existing))
	for _, rec := range existing {
		existingByDID[strings.TrimSpace(rec.Did)] = rec
	}

	out := make([]copyRecord, 0, len(source))
	for _, src := range source {
		did := strings.TrimSpace(src.Did)
		if did == "" {
			continue
		}
		if dstRec, ok := existingByDID[did]; ok {
			merged, changed := mergeExistingRecord(dstRec, src, overwriteNameFileName)
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

func mergeExistingRecord(dst, src copyRecord, overwriteNameFileName bool) (copyRecord, bool) {
	merged := dst
	changed := false

	if overwriteNameFileName {
		if !equalStringValuePointers(merged.Name, src.Name) {
			merged.Name = src.Name
			changed = true
		}
		if !equalStringValuePointers(merged.FileName, src.FileName) {
			merged.FileName = src.FileName
			changed = true
		}
	}

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

func equalStringValuePointers(a, b *string) bool {
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
