package drsremote

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drsobject"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/calypr/syfon/client/request"
	"github.com/calypr/syfon/client/transfer"
	sydownload "github.com/calypr/syfon/client/transfer/download"
)

func ObjectsByHash(ctx context.Context, drsCtx *config.GitContext, checksum string) ([]drsapi.DrsObject, error) {
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

func ObjectsByHashForScope(ctx context.Context, drsCtx *config.GitContext, checksum string) ([]drsapi.DrsObject, error) {
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

func AccessURLForHashScope(ctx context.Context, drsCtx *config.GitContext, checksum string) (*drsapi.AccessURL, *drsapi.DrsObject, error) {
	records, err := ObjectsByHashForScope(ctx, drsCtx, checksum)
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("no matching DRS record found for oid %s", drsobject.NormalizeChecksum(checksum))
	}
	match := records[0]
	if match.AccessMethods == nil || len(*match.AccessMethods) == 0 {
		return nil, nil, fmt.Errorf("no access methods available for DRS object %s", match.Id)
	}
	accessType := (*match.AccessMethods)[0].Type
	if accessType == "" {
		return nil, nil, fmt.Errorf("no access type found in access method for DRS object %s", match.Id)
	}
	accessURL, err := drsCtx.Client.DRS().GetAccessURL(ctx, match.Id, string(accessType))
	if err != nil {
		return nil, nil, err
	}
	return &accessURL, &match, nil
}

func BulkAccessURLsForObjects(ctx context.Context, drsCtx *config.GitContext, objects []drsapi.DrsObject) (map[string]drsapi.AccessURL, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	req, ok := bulkAccessRequest(objects)
	if !ok {
		return map[string]drsapi.AccessURL{}, nil
	}

	resp, err := drsCtx.Client.DRSAPI().GetBulkAccessURLWithResponse(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("unexpected response: %d", resp.StatusCode())
	}

	out := map[string]drsapi.AccessURL{}
	if resp.JSON200.ResolvedDrsObjectAccessUrls == nil {
		return out, nil
	}
	for _, resolved := range *resp.JSON200.ResolvedDrsObjectAccessUrls {
		if resolved.DrsObjectId == nil {
			continue
		}
		objectID := *resolved.DrsObjectId
		if strings.TrimSpace(objectID) == "" || strings.TrimSpace(resolved.Url) == "" {
			continue
		}
		out[strings.TrimSpace(objectID)] = drsapi.AccessURL{Headers: resolved.Headers, Url: resolved.Url}
	}
	return out, nil
}

// DownloadToCachePath downloads the DRS object identified by oid to cachePath
// using the project-scoped URL resolution preferred by git-drs.
func DownloadToCachePath(ctx context.Context, drsCtx *config.GitContext, logger *slog.Logger, oid, cachePath string) error {
	_ = logger
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}

	accessURL, match, err := AccessURLForHashScope(ctx, drsCtx, oid)
	if err != nil {
		return err
	}
	return downloadResolved(ctx, drsCtx, oid, cachePath, match, accessURL)
}

func DownloadResolvedToCachePath(ctx context.Context, drsCtx *config.GitContext, oid, cachePath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}
	if obj == nil || accessURL == nil || accessURL.Url == "" {
		return DownloadToCachePath(ctx, drsCtx, nil, oid, cachePath)
	}
	return downloadResolved(ctx, drsCtx, oid, cachePath, obj, accessURL)
}

func DownloadResolvedToPath(ctx context.Context, drsCtx *config.GitContext, oid, dstPath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL, opts sydownload.DownloadOptions) error {
	if drsCtx == nil || drsCtx.Client == nil {
		return fmt.Errorf("DRS client unavailable")
	}
	if obj == nil || accessURL == nil || strings.TrimSpace(accessURL.Url) == "" {
		return fmt.Errorf("resolved DRS object and access URL are required")
	}
	src := &resolvedSource{
		requestor:    drsCtx.Client.Requestor(),
		accessURL:    strings.TrimSpace(accessURL.Url),
		expectedSize: obj.Size,
	}
	return sydownload.DownloadToPathWithOptions(ctx, src, oid, dstPath, opts)
}

func downloadResolved(ctx context.Context, drsCtx *config.GitContext, oid, cachePath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL) error {
	return DownloadResolvedToPath(ctx, drsCtx, oid, cachePath, obj, accessURL, sydownload.DownloadOptions{
		MultipartThreshold: 5 * 1024 * 1024,
		Concurrency:        2,
		ChunkSize:          64 * 1024 * 1024,
	})
}

type resolvedSource struct {
	requestor    request.Requester
	accessURL    string
	expectedSize int64
}

func (s *resolvedSource) Name() string {
	return "resolved-url"
}

func (s *resolvedSource) Logger() transfer.TransferLogger {
	return transfer.NoOpLogger{}
}

func (s *resolvedSource) Stat(ctx context.Context, guid string) (*transfer.ObjectMetadata, error) {
	return &transfer.ObjectMetadata{
		Size:         s.expectedSize,
		AcceptRanges: s.expectedSize > 0,
		Provider:     "drs",
	}, nil
}

func (s *resolvedSource) GetReader(ctx context.Context, guid string) (io.ReadCloser, error) {
	return s.download(ctx, nil, nil)
}

func (s *resolvedSource) GetRangeReader(ctx context.Context, guid string, offset, length int64) (io.ReadCloser, error) {
	if length <= 0 {
		return s.download(ctx, nil, nil)
	}
	end := offset + length - 1
	return s.download(ctx, &offset, &end)
}

func (s *resolvedSource) download(ctx context.Context, start, end *int64) (io.ReadCloser, error) {
	resp, err := transfer.GenericDownload(ctx, s.requestor, s.accessURL, start, end)
	if err != nil {
		return nil, err
	}
	if start != nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil, transfer.ErrRangeIgnored
	}
	return resp.Body, nil
}
