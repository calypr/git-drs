package lookup

import (
	"context"
	"fmt"
	"sync"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"golang.org/x/sync/errgroup"
)

func ObjectsByHash(ctx context.Context, drsCtx *remoteruntime.GitContext, checksum string) ([]drsapi.DrsObject, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	checksum = drsobject.NormalizeChecksum(checksum)
	if checksum == "" {
		return nil, nil
	}
	page, err := drsCtx.Client.DRS().BatchGetObjectsByHash(ctx, []string{checksum})
	if err != nil {
		return nil, err
	}
	return page.DrsObjects, nil
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

	var mu sync.Mutex
	drsObjects := make([]drsapi.DrsObject, 0)
	concurrency := 20
	sem := make(chan struct{}, concurrency)
	g, gCtx := errgroup.WithContext(ctx)

	for _, checksum := range queryChecksums {
		checksum := checksum
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			resp, err := drsCtx.Client.DRSAPI().GetObjectsByChecksumWithResponse(gCtx, drsapi.ChecksumParameter(checksum))
			if err != nil {
				return fmt.Errorf("get objects by checksum %s: %w", checksum, err)
			}
			if resp.JSON200 == nil || resp.JSON200.ResolvedDrsObject == nil {
				return fmt.Errorf("get objects by checksum %s failed: unexpected response: %d", checksum, resp.StatusCode())
			}

			mu.Lock()
			drsObjects = append(drsObjects, *resp.JSON200.ResolvedDrsObject...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	results := make(map[string][]drsapi.DrsObject, len(normalizedToOriginal))
	for normalized, original := range normalizedToOriginal {
		results[original] = nil
		results[normalized] = nil
	}
	for _, obj := range drsObjects {
		for _, checksum := range obj.Checksums {
			if checksum.Type == "" || checksum.Checksum == "" {
				continue
			}
			normalized := drsobject.NormalizeChecksum(fmt.Sprintf("%s:%s", checksum.Type, checksum.Checksum))
			if normalized == "" {
				continue
			}
			original, ok := normalizedToOriginal[normalized]
			if !ok {
				continue
			}
			results[original] = append(results[original], obj)
			if original != normalized {
				results[normalized] = append(results[normalized], obj)
			}
		}
	}

	return results, nil
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
