package copyrecords

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	drsapi "github.com/calypr/syfon/apigen/drs"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
)

// RegisterObjects merges existing records by DID or checksum and registers them in batches.
func RegisterObjects(ctx context.Context, client *internalapi.ClientWithResponses, objects []*drsapi.DrsObject, organization, project string, logger *slog.Logger) error {
	records := make([]copyRecord, 0, len(objects))
	for _, obj := range objects {
		record := copyRecordFromLocalObject(obj)
		record.CreatedTime = nil
		record.Organization = &organization
		record.Project = &project
		records = append(records, record)
	}
	api := newRawIndexAPI(client)
	if _, err := copyProjectRecordsWithOptions(ctx, logger, records, api, organization, project, defaultCopyBatchSize, false, false); err != nil {
		return err
	}
	for start := 0; start < len(objects); start += defaultCopyBatchSize {
		end := min(start+defaultCopyBatchSize, len(objects))
		queries := make([]string, 0, end-start)
		for _, record := range records[start:end] {
			queries = append(queries, "sha256:"+copyRecordSHA256(record))
		}
		result, err := api.BulkHashes(ctx, queries)
		if err != nil {
			return fmt.Errorf("verify registered records: %w", err)
		}
		for i, query := range queries {
			matches := dedupeCopyRecordsByDID(result.Results[query])
			var id string
			for _, match := range matches {
				if match.Did == records[start+i].Did {
					id = match.Did
					break
				}
			}
			if id == "" && len(matches) == 1 {
				id = matches[0].Did
			}
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("cannot resolve unique registered record for %s", query)
			}
			objects[start+i].Id = id
		}
	}
	return nil
}

// VerifyObjects confirms existing records match the requested locations and scope without writing them.
func VerifyObjects(ctx context.Context, client *internalapi.ClientWithResponses, objects []*drsapi.DrsObject) error {
	api := newRawIndexAPI(client)
	for start := 0; start < len(objects); start += defaultCopyBatchSize {
		end := min(start+defaultCopyBatchSize, len(objects))
		queries := make([]string, 0, end-start)
		for _, obj := range objects[start:end] {
			queries = append(queries, "sha256:"+copyRecordSHA256(copyRecordFromLocalObject(obj)))
		}
		result, err := api.BulkHashes(ctx, queries)
		if err != nil {
			return err
		}
		for i, query := range queries {
			wanted := objects[start+i]
			found := false
			for _, match := range result.Results[query] {
				if match.Size == nil || *match.Size != wanted.Size || match.AccessMethods == nil || wanted.AccessMethods == nil || wanted.ControlledAccess == nil || match.ControlledAccess == nil {
					continue
				}
				scopeFound, urlFound := false, false
				for _, scope := range *wanted.ControlledAccess {
					for _, existing := range *match.ControlledAccess {
						if scope == existing {
							scopeFound = true
						}
					}
				}
				for _, method := range *wanted.AccessMethods {
					for _, existing := range *match.AccessMethods {
						if method.AccessUrl != nil && existing.AccessUrl != nil && method.AccessUrl.Url == existing.AccessUrl.Url {
							urlFound = true
						}
					}
				}
				if scopeFound && urlFound {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("no existing record with matching size, scope and storage location for %s", query)
			}
		}
	}
	return nil
}
