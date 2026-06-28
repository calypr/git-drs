package copyrecords

import (
	"context"
	"fmt"
	"os"
	"strings"

	syservices "github.com/calypr/syfon/client/services"
	sycommon "github.com/calypr/syfon/common"
)

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
		fmt.Fprintf(os.Stderr, "copy-records: scanning source index page %d, matched-so-far=%d\n", page, len(out))
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
