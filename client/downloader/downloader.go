package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	dcrequest "github.com/calypr/data-client/request"
	"github.com/calypr/git-drs/client"
	clientdrs "github.com/calypr/git-drs/client/drs"
	"github.com/calypr/git-drs/drsmap"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/transfer"
	"github.com/calypr/syfon/client/xfer/download"
)

// DownloadToCachePath downloads the DRS object identified by oid to cachePath
// using the project-scoped URL resolution preferred by git-drs.
func DownloadToCachePath(ctx context.Context, drsCtx *client.GitContext, logger *slog.Logger, oid, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}

	backend, err := newScopedBackend(ctx, drsCtx, logger, oid)
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
	requestor dcrequest.RequestInterface
	object    *drsapi.DrsObject
	accessURL string
}

func newScopedBackend(ctx context.Context, drsCtx *client.GitContext, logger *slog.Logger, oid string) (*scopedBackend, error) {
	if drsCtx == nil || drsCtx.API == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}

	records, err := clientdrs.GetObjectByHashForGit(ctx, drsCtx.API, oid, drsCtx.Organization, drsCtx.ProjectId)
	if err != nil {
		return nil, err
	}
	match, err := drsmap.FindMatchingRecord(records, drsCtx.Organization, drsCtx.ProjectId)
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, fmt.Errorf("no matching DRS record found for oid %s", oid)
	}

	accessURL, err := clientdrs.ResolveGitScopedURL(ctx, drsCtx.API, oid, drsCtx.Organization, drsCtx.ProjectId, logger)
	if err != nil {
		return nil, err
	}

	return &scopedBackend{
		base:      drsCtx.API.SyfonClient().Data(),
		requestor: drsCtx.API,
		object:    match,
		accessURL: strings.TrimSpace(accessURL.Url),
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
	rb := b.requestor.New(http.MethodGet, b.accessURL)
	if rangeStart != nil {
		rangeHeader := "bytes=" + fmt.Sprintf("%d-", *rangeStart)
		if rangeEnd != nil {
			rangeHeader += fmt.Sprintf("%d", *rangeEnd)
		}
		rb.WithHeader("Range", rangeHeader)
	}
	if sycommon.IsCloudPresignedURL(b.accessURL) {
		rb.WithSkipAuth(true)
	}
	resp, err := b.requestor.Do(ctx, rb)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("download failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}
