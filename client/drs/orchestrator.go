package drs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/syfon/client/drs"
	"github.com/calypr/syfon/client/hash"
	"github.com/calypr/syfon/client/transfer"
	"github.com/calypr/syfon/client/xfer/download"
)

var ErrNoRecordsForOID = errors.New("no records found for OID")

// ResolveGitScopedURL implements the specialized git-drs logic of performing an
// issuer-filtered hash lookup to find the appropriate download record.
func ResolveGitScopedURL(
	ctx context.Context,
	api drs.Client,
	oid string,
	organization string,
	projectId string,
	logger *slog.Logger,
) (*drs.AccessURL, error) {
	logger.Debug(fmt.Sprintf("Try to get download url for file OID %s", oid))

	records, err := GetObjectByHashForGit(ctx, api, oid, organization, projectId)
	if err != nil {
		logger.Debug(fmt.Sprintf("error getting DRS object for OID %s: %s", oid, err))
		return nil, fmt.Errorf("error getting DRS object for OID %s: %v", oid, err)
	}

	if len(records) == 0 {
		logger.Debug(fmt.Sprintf("no DRS object found for OID %s", oid))
		return nil, fmt.Errorf("no DRS object found for OID %s", oid)
	}

	matchingRecord, err := drsmap.FindMatchingRecord(records, organization, projectId)
	if err != nil {
		logger.Debug(fmt.Sprintf("error finding matching record for project %s: %s", projectId, err))
		return nil, fmt.Errorf("error finding matching record for project %s: %v", projectId, err)
	}
	if matchingRecord == nil {
		logger.Debug(fmt.Sprintf("no matching record found for project %s", projectId))
		return nil, fmt.Errorf("no matching record found for project %s", projectId)
	}

	logger.Debug(fmt.Sprintf("Matching record: %#v for oid %s", matchingRecord, oid))

	if len(matchingRecord.AccessMethods) == 0 {
		return nil, fmt.Errorf("no access methods available for DRS object %s", matchingRecord.Id)
	}

	accessType := matchingRecord.AccessMethods[0].Type
	if accessType == "" {
		return nil, fmt.Errorf("no accessType found in access method for DRS object %v", matchingRecord.AccessMethods[0])
	}

	return api.GetDownloadURL(ctx, matchingRecord.Id, accessType)
}

// GetObjectByHashForGit queries for an object by hash but uniquely filters
// the results based on the BearerAuthIssuers intersecting with the Git scopes.
func GetObjectByHashForGit(
	ctx context.Context,
	api drs.Client,
	oid string,
	organization string,
	projectId string,
) ([]drs.DRSObject, error) {
	sum := &hash.Checksum{Type: string(hash.ChecksumTypeSHA256), Checksum: oid}
	res, err := api.GetObjectByHash(ctx, sum)
	if err != nil {
		return nil, err
	}

	resourcePath, err := drs.ProjectToResource(organization, projectId)
	if err != nil {
		return nil, err
	}

	filtered := make([]drs.DRSObject, 0)
	for _, o := range res {
		found := false
		for _, am := range o.AccessMethods {
			for _, issuer := range am.Authorizations.BearerAuthIssuers {
				if issuer == resourcePath {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			filtered = append(filtered, o)
		}
	}
	return filtered, nil
}

// DeleteRecordsByOID sweeps and deletes all DIDs matching a git OID hash.
func DeleteRecordsByOID(ctx context.Context, api drs.Client, oid string) error {
	records, err := api.GetObjectByHash(ctx, &hash.Checksum{Type: string(hash.ChecksumTypeSHA256), Checksum: oid})
	if err != nil {
		return fmt.Errorf("error resolving DRS object for OID %s: %w", oid, err)
	}
	if len(records) == 0 {
		return fmt.Errorf("%w %s", ErrNoRecordsForOID, oid)
	}

	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		did := strings.TrimSpace(record.Id)
		if did == "" {
			continue
		}
		if _, exists := seen[did]; exists {
			continue
		}
		seen[did] = struct{}{}
		if err := api.DeleteRecord(ctx, did); err != nil {
			return fmt.Errorf("error deleting DID %s for OID %s: %w", did, oid, err)
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("no deleteable DIDs found for OID %s", oid)
	}
	return nil
}

func BuildDrsObj(fileName string, checksum string, size int64, drsId string, bucket string, org string, projectId string, prefix string) (*drs.DRSObject, error) {
	return drs.BuildDrsObjWithPrefix(fileName, checksum, size, drsId, bucket, org, projectId, prefix)
}

func DownloadFile(ctx context.Context, api drs.Client, reader transfer.Downloader, logger *slog.Logger, oid string, destPath string, multiPartThreshold int64) error {
	opts := download.DownloadOptions{
		MultipartThreshold: 5 * 1024 * 1024,
	}
	if multiPartThreshold > 0 {
		opts.MultipartThreshold = multiPartThreshold
	}
	return download.DownloadToPathWithOptions(ctx, api, reader, logger, oid, destPath, "", opts)
}
