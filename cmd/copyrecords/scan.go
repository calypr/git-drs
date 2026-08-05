package copyrecords

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syservices "github.com/calypr/syfon/client/services"
	sycommon "github.com/calypr/syfon/common"
	"github.com/google/uuid"
)

var (
	loadTrackedLfsFiles = lfs.GetTrackedLfsFiles
	readLocalDRSObject  = func(oid string) (*drsapi.DrsObject, error) {
		return drsobject.ReadObject(gitrepo.DRSObjectsPath, oid)
	}
	writeLocalDRSObject = func(oid string, obj *drsapi.DrsObject) error {
		return drsobject.WriteObject(gitrepo.DRSObjectsPath, obj, oid)
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
	if _, err := sycommon.ResourcePath(org, project); err != nil {
		return nil, fmt.Errorf("invalid scope %s/%s: %w", org, project, err)
	}
	batchSize = normalizeCopyBatchSize(batchSize)

	page := 1
	startAfter := ""
	out := make([]copyRecord, 0)
	seen := map[string]struct{}{}
	for {
		fmt.Fprintf(os.Stderr, "copy-records: scanning source index page %d for %s/%s, start-after=%q matched-so-far=%d\n", page, org, project, startAfter, len(out))
		records, err := listSourceRecordPage(ctx, src, org, project, batchSize, startAfter)
		if err != nil {
			return nil, err
		}
		if len(records) == 0 {
			break
		}
		nextStartAfter := lastCopyRecordDID(records)
		if nextStartAfter == "" {
			return nil, fmt.Errorf("source list for %s/%s returned records without DIDs; cannot advance cursor", org, project)
		}
		for _, rec := range records {
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
		startAfter = nextStartAfter
		page++
	}
	return out, nil
}

func listSourceRecordPage(ctx context.Context, src indexAPI, org, project string, batchSize int, startAfter string) ([]copyRecord, error) {
	listResp, err := src.List(ctx, syservices.ListRecordsOptions{
		Organization: org,
		ProjectID:    project,
		Limit:        batchSize,
		Start:        startAfter,
	})
	if err != nil {
		return nil, fmt.Errorf("source list failed for %s/%s start-after %q: %w", org, project, startAfter, err)
	}
	if listResp.Records == nil {
		return nil, nil
	}
	return *listResp.Records, nil
}

func lastCopyRecordDID(records []copyRecord) string {
	for i := len(records) - 1; i >= 0; i-- {
		if did := strings.TrimSpace(records[i].Did); did != "" {
			return did
		}
	}
	return ""
}

func loadLocalSourceRecords(org, project string) ([]copyRecord, error) {
	return loadLocalSourceRecordsIncluding(org, project, nil)
}

func loadLocalSourceRecordsIncluding(org, project string, include []string) ([]copyRecord, error) {
	resource, err := sycommon.ResourcePath(org, project)
	if err != nil {
		return nil, fmt.Errorf("invalid scope %s/%s: %w", org, project, err)
	}

	inventory, err := loadTrackedLfsFiles(drslog.NewNoOpLogger())
	if err != nil {
		return nil, fmt.Errorf("load tracked LFS files: %w", err)
	}
	include, err = normalizeIncludedPaths(include)
	if err != nil {
		return nil, err
	}

	out := make([]copyRecord, 0, len(inventory))
	seenOIDs := make(map[string]struct{}, len(inventory))
	for path, info := range inventory {
		if !isIncludedLocalPath(path, include) {
			continue
		}
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
			obj, err = localDRSObjectFromPayload(path, info, oid)
			if err != nil {
				return nil, fmt.Errorf("tracked oid %s for path %s is missing local DRS metadata and no matching local payload was found: %w", oid, path, err)
			}
			fmt.Fprintf(os.Stderr, "copy-records: reconstructed missing local metadata for %s from verified payload\n", path)
		}
		out = append(out, rewriteCopyRecordScope(copyRecordFromLocalObject(obj), org, project, resource))
	}
	return out, nil
}

func includedLocalSHA256(include []string) (map[string]struct{}, error) {
	include, err := normalizeIncludedPaths(include)
	if err != nil {
		return nil, err
	}
	inventory, err := loadTrackedLfsFiles(drslog.NewNoOpLogger())
	if err != nil {
		return nil, fmt.Errorf("load tracked LFS files for --include-path: %w", err)
	}
	hashes := make(map[string]struct{})
	matchedPaths := 0
	for path, info := range inventory {
		if !isIncludedLocalPath(path, include) {
			continue
		}
		matchedPaths++
		oid := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(info.Oid, "sha256:")))
		if oid != "" {
			hashes[oid] = struct{}{}
		}
	}
	if matchedPaths == 0 {
		return nil, fmt.Errorf("--include-path matched no tracked files in the current repository")
	}
	if len(hashes) == 0 {
		return nil, fmt.Errorf("files matched by --include-path have no SHA-256 OIDs")
	}
	return hashes, nil
}

func normalizeIncludedPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(raw)))
		path = strings.TrimPrefix(path, "./")
		if path == "" || path == "." {
			return nil, fmt.Errorf("invalid --include-path %q: expected a repository-relative file or directory", raw)
		}
		if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, "../") {
			return nil, fmt.Errorf("invalid --include-path %q: path must stay within the repository", raw)
		}
		out = append(out, strings.TrimSuffix(path, "/"))
	}
	return out, nil
}

func isIncludedLocalPath(path string, include []string) bool {
	if len(include) == 0 {
		return true
	}
	path = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
	for _, prefix := range include {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func localDRSObjectFromPayload(path string, info lfs.LfsFileInfo, oid string) (*drsapi.DrsObject, error) {
	candidates := []string{path}
	if cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, oid); err == nil {
		candidates = append([]string{cachePath}, candidates...)
	}
	for _, candidate := range candidates {
		stat, err := os.Stat(candidate)
		if os.IsNotExist(err) || (err == nil && (stat.IsDir() || stat.Size() != info.Size)) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stat payload %s: %w", candidate, err)
		}
		matches, err := lfs.FileMatchesSHA256(candidate, oid)
		if err != nil {
			return nil, fmt.Errorf("hash payload %s: %w", candidate, err)
		}
		if matches {
			name := filepath.Base(path)
			return &drsapi.DrsObject{Name: &name, Size: stat.Size(), Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}}}, nil
		}
	}
	return nil, fmt.Errorf("expected sha256 %s and size %d", oid, info.Size)
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
		AccessMethods:    obj.AccessMethods,
		ControlledAccess: obj.ControlledAccess,
		CreatedTime:      stringTimePointer(obj.CreatedTime.String()),
		Description:      obj.Description,
		Did:              strings.TrimSpace(obj.Id),
		Name:             obj.Name,
		Size:             int64Pointer(obj.Size),
		Version:          obj.Version,
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
	if strings.TrimSpace(record.Did) == "" {
		if sha := copyRecordSHA256(record); sha != "" {
			record.Did = uuid.NewSHA1(drsobject.UUIDNamespace, []byte(fmt.Sprintf("%s:%s", project, drsobject.NormalizeOid(sha)))).String()
		}
	}
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
