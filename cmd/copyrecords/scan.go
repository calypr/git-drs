package copyrecords

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syservices "github.com/calypr/syfon/client/services"
	sycommon "github.com/calypr/syfon/common"
)

var (
	loadTrackedLfsFiles = lfs.GetTrackedLfsFiles
	readLocalDRSObject  = func(oid string) (*drsapi.DrsObject, error) {
		return drsobject.ReadObject(gitrepo.DRSObjectsPath, oid)
	}
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

func loadLocalSourceRecords(org, project string) ([]copyRecord, error) {
	resource, err := sycommon.ResourcePath(org, project)
	if err != nil {
		return nil, fmt.Errorf("invalid scope %s/%s: %w", org, project, err)
	}

	inventory, err := loadTrackedLfsFiles(drslog.NewNoOpLogger())
	if err != nil {
		return nil, fmt.Errorf("load tracked LFS files: %w", err)
	}

	out := make([]copyRecord, 0, len(inventory))
	seenOIDs := make(map[string]struct{}, len(inventory))
	for path, info := range inventory {
		oid := strings.TrimSpace(strings.TrimPrefix(info.Oid, "sha256:"))
		if oid == "" {
			continue
		}
		if _, seen := seenOIDs[oid]; seen {
			continue
		}
		seenOIDs[oid] = struct{}{}

		obj, err := readLocalDRSObject(oid)
		if err != nil {
			return nil, fmt.Errorf("tracked oid %s for path %s is missing local DRS metadata: %w", oid, path, err)
		}
		out = append(out, rewriteCopyRecordScope(copyRecordFromLocalObject(obj), org, project, resource))
	}
	return out, nil
}

func copyRecordFromLocalObject(obj *drsapi.DrsObject) copyRecord {
	if obj == nil {
		return copyRecord{}
	}
	hashes := copyHashInfo{}
	for _, checksum := range obj.Checksums {
		typ := strings.TrimSpace(checksum.Type)
		sum := strings.TrimSpace(checksum.Checksum)
		if typ == "" || sum == "" {
			continue
		}
		hashes[typ] = sum
	}

	record := copyRecord{
		AccessMethods: obj.AccessMethods,
		CreatedTime:   stringTimePointer(obj.CreatedTime.String()),
		Description:   obj.Description,
		Did:           strings.TrimSpace(obj.Id),
		Name:          obj.Name,
		Size:          int64Pointer(obj.Size),
		Version:       obj.Version,
	}
	if len(hashes) > 0 {
		record.Hashes = &hashes
	}
	if obj.UpdatedTime != nil {
		record.UpdatedTime = stringTimePointer(obj.UpdatedTime.String())
	}
	return record
}

func rewriteCopyRecordScope(record copyRecord, org, project, resource string) copyRecord {
	record.Organization = stringPointer(org)
	record.Project = stringPointer(project)
	if resource == "" {
		record.ControlledAccess = nil
		return record
	}
	record.ControlledAccess = &[]string{resource}
	return record
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringTimePointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" || value == "0001-01-01 00:00:00 +0000 UTC" {
		return nil
	}
	return &value
}

func int64Pointer(value int64) *int64 {
	return &value
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
