package lookup

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	internalapi "github.com/calypr/syfon/apigen/client/internalapi"
	syfoncommon "github.com/calypr/syfon/common"
)

func ObjectsByHash(ctx context.Context, drsCtx *remoteruntime.GitContext, checksum string) ([]drsapi.DrsObject, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	checksum = drsobject.NormalizeChecksum(checksum)
	if checksum == "" {
		return nil, nil
	}
	objects, err := ObjectsByHashes(ctx, drsCtx, []string{checksum})
	if err != nil {
		return nil, err
	}
	return objects[checksum], nil
}

func ObjectsByHashes(ctx context.Context, drsCtx *remoteruntime.GitContext, checksums []string) (map[string][]drsapi.DrsObject, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	normalizedToOriginal := make(map[string]string, len(checksums))
	queryChecksums := make([]string, 0, len(checksums))
	for _, checksum := range checksums {
		normalized := drsobject.NormalizeChecksum(checksum)
		if normalized == "" {
			continue
		}
		if _, exists := normalizedToOriginal[normalized]; exists {
			continue
		}
		normalizedToOriginal[normalized] = checksum
		queryChecksums = append(queryChecksums, normalized)
	}
	if len(queryChecksums) == 0 {
		return map[string][]drsapi.DrsObject{}, nil
	}

	var response struct {
		Results map[string][]internalapi.InternalRecord `json:"results"`
	}
	if err := drsCtx.Client.Requestor().Do(ctx, http.MethodPost, "/index/bulk/hashes", internalapi.BulkHashesRequest{Hashes: queryChecksums}, &response); err != nil {
		return nil, fmt.Errorf("batch objects by checksum: %w", err)
	}

	results := make(map[string][]drsapi.DrsObject, len(normalizedToOriginal))
	for normalized, original := range normalizedToOriginal {
		objects := make([]drsapi.DrsObject, 0, len(response.Results[normalized]))
		for _, record := range response.Results[normalized] {
			objects = append(objects, internalRecordToDRSObject(record))
		}
		results[original] = objects
		if original != normalized {
			results[normalized] = objects
		}
	}

	return results, nil
}

func internalRecordToDRSObject(record internalapi.InternalRecord) drsapi.DrsObject {
	obj := drsapi.DrsObject{Id: record.Did, SelfUri: "drs://" + record.Did}
	if record.Size != nil {
		obj.Size = *record.Size
	}
	// Prefer a provided name.
	name := record.Name
	if name != nil {
		base := path.Base(strings.TrimSpace(*name))
		if base == "." || base == "/" || base == "" {
			base = strings.TrimSpace(*name)
		}
		obj.Name = &base
	}
	if record.Hashes != nil {
		obj.Checksums = make([]drsapi.Checksum, 0, len(*record.Hashes))
		for typ, checksum := range *record.Hashes {
			obj.Checksums = append(obj.Checksums, drsapi.Checksum{Type: typ, Checksum: checksum})
		}
	}
	if record.ControlledAccess != nil {
		controlled := syfoncommon.NormalizeAccessResources(*record.ControlledAccess)
		obj.ControlledAccess = &controlled
	}
	if record.AccessMethods != nil {
		methods := append([]drsapi.AccessMethod(nil), (*record.AccessMethods)...)
		obj.AccessMethods = &methods
	}
	// Preserve representable metadata fields from the internal record.
	if record.Description != nil {
		obj.Description = record.Description
	}
	if record.Version != nil {
		obj.Version = record.Version
	}
	if record.CreatedTime != nil {
		if t, err := time.Parse(time.RFC3339, *record.CreatedTime); err == nil {
			obj.CreatedTime = t
		}
	}
	if record.UpdatedTime != nil {
		if t, err := time.Parse(time.RFC3339, *record.UpdatedTime); err == nil {
			obj.UpdatedTime = &t
		}
	}
	return obj
}

func ObjectsByHashForScope(ctx context.Context, drsCtx *remoteruntime.GitContext, checksum string) ([]drsapi.DrsObject, error) {
	objects, err := ObjectsByHash(ctx, drsCtx, checksum)
	if err != nil {
		return nil, err
	}
	result := make([]drsapi.DrsObject, 0, len(objects))
	for _, obj := range objects {
		if MatchesScope(&obj, drsCtx.Organization, drsCtx.ProjectId) {
			result = append(result, obj)
		}
	}
	return result, nil
}

func ObjectsByHashesForScope(ctx context.Context, drsCtx *remoteruntime.GitContext, checksums []string) (map[string][]drsapi.DrsObject, error) {
	objectsByChecksum, err := ObjectsByHashes(ctx, drsCtx, checksums)
	if err != nil {
		return nil, err
	}
	results := make(map[string][]drsapi.DrsObject, len(objectsByChecksum))
	for checksum, objects := range objectsByChecksum {
		filtered := make([]drsapi.DrsObject, 0, len(objects))
		for _, obj := range objects {
			if MatchesScope(&obj, drsCtx.Organization, drsCtx.ProjectId) {
				filtered = append(filtered, obj)
			}
		}
		results[checksum] = filtered
	}
	return results, nil
}
