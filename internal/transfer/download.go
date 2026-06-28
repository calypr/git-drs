package transfer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/request"
	sytransfer "github.com/calypr/syfon/client/transfer"
	sydownload "github.com/calypr/syfon/client/transfer/download"
)

func AccessURLForHashScope(ctx context.Context, drsCtx *remoteruntime.GitContext, checksum string) (*drsapi.AccessURL, *drsapi.DrsObject, error) {
	records, err := lookup.ObjectsByHashForScope(ctx, drsCtx, checksum)
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

func DownloadToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}

	accessURL, match, err := AccessURLForHashScope(ctx, drsCtx, oid)
	if err != nil {
		return err
	}
	return downloadResolved(ctx, drsCtx, oid, cachePath, match, accessURL)
}

func DownloadResolvedToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, cachePath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}
	if obj == nil || accessURL == nil || accessURL.Url == "" {
		return DownloadToCachePath(ctx, drsCtx, oid, cachePath)
	}
	return downloadResolved(ctx, drsCtx, oid, cachePath, obj, accessURL)
}

func DownloadResolvedToPath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, dstPath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL, opts sydownload.DownloadOptions) error {
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

func downloadResolved(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, cachePath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL) error {
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

func (s *resolvedSource) Logger() sytransfer.TransferLogger {
	return sytransfer.NoOpLogger{}
}

func (s *resolvedSource) Stat(ctx context.Context, guid string) (*sytransfer.ObjectMetadata, error) {
	return &sytransfer.ObjectMetadata{
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
	resp, err := sytransfer.GenericDownload(ctx, s.requestor, s.accessURL, start, end)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		err := sycommon.ResponseBodyError(resp, fmt.Sprintf("download from %s failed", s.accessURL))
		resp.Body.Close()
		return nil, err
	}
	if start != nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil, sytransfer.ErrRangeIgnored
	}
	return resp.Body, nil
}
