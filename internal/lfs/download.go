package lfs

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
	"github.com/calypr/git-drs/internal/drslookup"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sycommon "github.com/calypr/syfon/client/common"
	syrequest "github.com/calypr/syfon/client/request"
	"github.com/calypr/syfon/client/transfer"
	"github.com/calypr/syfon/client/xfer/download"
)

// DownloadToCachePath downloads the DRS object identified by oid to cachePath
// using the project-scoped URL resolution preferred by git-drs.
func DownloadToCachePath(ctx context.Context, drsCtx *config.GitContext, logger *slog.Logger, oid, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}

	backend, err := newScopedBackend(ctx, drsCtx, oid)
	if err != nil {
		return err
	}

	opts := download.DownloadOptions{
		MultipartThreshold: 5 * 1024 * 1024,
		Concurrency:        2,
		ChunkSize:          64 * 1024 * 1024,
	}
	return download.DownloadToPathWithOptions(ctx, backend, oid, cachePath, opts)
}

func DownloadResolvedToCachePath(ctx context.Context, drsCtx *config.GitContext, oid, cachePath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}
	if obj == nil || accessURL == nil || strings.TrimSpace(accessURL.Url) == "" {
		return DownloadToCachePath(ctx, drsCtx, nil, oid, cachePath)
	}
	backend, err := newScopedBackendFromResolved(drsCtx, obj, strings.TrimSpace(accessURL.Url))
	if err != nil {
		return err
	}

	opts := download.DownloadOptions{
		MultipartThreshold: 5 * 1024 * 1024,
		Concurrency:        2,
		ChunkSize:          64 * 1024 * 1024,
	}
	return download.DownloadToPathWithOptions(ctx, backend, oid, cachePath, opts)
}

type scopedBackend struct {
	base      transfer.Backend
	requestor syrequest.Requester
	object    *drsapi.DrsObject
	accessURL string
}

func newScopedBackend(ctx context.Context, drsCtx *config.GitContext, oid string) (*scopedBackend, error) {
	if drsCtx == nil || drsCtx.Client == nil || drsCtx.Requestor == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}

	accessURL, match, err := drslookup.AccessURLForHashScope(ctx, drsCtx, oid)
	if err != nil {
		return nil, err
	}

	return &scopedBackend{
		base:      drsCtx.Client.Data(),
		requestor: drsCtx.Requestor,
		object:    match,
		accessURL: strings.TrimSpace(accessURL.Url),
	}, nil
}

func newScopedBackendFromResolved(drsCtx *config.GitContext, obj *drsapi.DrsObject, accessURL string) (*scopedBackend, error) {
	if drsCtx == nil || drsCtx.Client == nil || drsCtx.Requestor == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	return &scopedBackend{
		base:      drsCtx.Client.Data(),
		requestor: drsCtx.Requestor,
		object:    obj,
		accessURL: strings.TrimSpace(accessURL),
	}, nil
}

func (b *scopedBackend) Name() string { return b.base.Name() }

func (b *scopedBackend) Logger() transfer.TransferLogger { return b.base.Logger() }

func (b *scopedBackend) Validate(ctx context.Context, bucket string) error {
	return b.base.Validate(ctx, bucket)
}

func (b *scopedBackend) Stat(ctx context.Context, guid string) (*transfer.ObjectMetadata, error) {
	if b.object == nil {
		return b.base.Stat(ctx, guid)
	}
	md := &transfer.ObjectMetadata{
		Size:         b.object.Size,
		AcceptRanges: true,
		Provider:     "drs",
	}
	return md, nil
}

func (b *scopedBackend) GetReader(ctx context.Context, guid string) (io.ReadCloser, error) {
	return b.download(ctx, nil, nil)
}

func (b *scopedBackend) GetRangeReader(ctx context.Context, guid string, offset, length int64) (io.ReadCloser, error) {
	if length <= 0 {
		return b.download(ctx, nil, nil)
	}
	end := offset + length - 1
	return b.download(ctx, &offset, &end)
}

func (b *scopedBackend) GetWriter(ctx context.Context, guid string) (io.WriteCloser, error) {
	return b.base.GetWriter(ctx, guid)
}

func (b *scopedBackend) Upload(ctx context.Context, guid string, body io.Reader, size int64) error {
	return b.base.Upload(ctx, guid, body, size)
}

func (b *scopedBackend) MultipartInit(ctx context.Context, guid string) (string, error) {
	return b.base.MultipartInit(ctx, guid)
}

func (b *scopedBackend) MultipartPart(ctx context.Context, guid string, uploadID string, partNum int, body io.Reader) (string, error) {
	return b.base.MultipartPart(ctx, guid, uploadID, partNum, body)
}

func (b *scopedBackend) MultipartComplete(ctx context.Context, guid string, uploadID string, parts []transfer.MultipartPart) error {
	return b.base.MultipartComplete(ctx, guid, uploadID, parts)
}

func (b *scopedBackend) Delete(ctx context.Context, guid string) error {
	return b.base.Delete(ctx, guid)
}

func (b *scopedBackend) download(ctx context.Context, rangeStart, rangeEnd *int64) (io.ReadCloser, error) {
	if b.accessURL == "" {
		return nil, fmt.Errorf("empty download URL")
	}
	var opts []syrequest.RequestOption
	if rangeStart != nil {
		rangeHeader := "bytes=" + fmt.Sprintf("%d-", *rangeStart)
		if rangeEnd != nil {
			rangeHeader += fmt.Sprintf("%d", *rangeEnd)
		}
		opts = append(opts, syrequest.WithHeader("Range", rangeHeader))
	}
	if sycommon.IsCloudPresignedURL(b.accessURL) {
		opts = append(opts, syrequest.WithSkipAuth(true))
	}
	var resp *http.Response
	if err := b.requestor.Do(ctx, http.MethodGet, b.accessURL, nil, &resp, opts...); err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}
