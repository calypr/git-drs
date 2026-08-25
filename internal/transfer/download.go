package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/lfs"
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
		return nil, nil, fmt.Errorf("AccessURLForHashScope: no access methods *available* for DRS object %s", match.Id)
	}
	accessURL, err := planAccessURL(ctx, drsCtx, match)
	if err != nil {
		return nil, nil, err
	}
	return accessURL, &match, nil
}

func AccessURLForDRSURI(ctx context.Context, drsCtx *remoteruntime.GitContext, drsURI string) (*drsapi.AccessURL, *drsapi.DrsObject, error) {
	if strings.HasPrefix(drsURI, "//") {
		drsURI = "drs:" + drsURI
	}
	obj, err := drsCtx.Client.DRS().GetObject(ctx, drsURI)
	if err != nil {
		return nil, nil, err
	}
	if obj.AccessMethods == nil || len(*obj.AccessMethods) == 0 {
		return nil, nil, fmt.Errorf("AccessURLForDRSURI: no access methods *available* for DRS object %s", obj.Id)
	}
	accessURL, err := planAccessURL(ctx, drsCtx, obj)
	if err != nil {
		return nil, nil, err
	}
	return accessURL, &obj, nil
}

func DownloadDRSURIToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, drsURI, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}
	accessURL, obj, err := AccessURLForDRSURI(ctx, drsCtx, drsURI)
	if err != nil {
		return err
	}
	return DownloadResolvedToCachePath(ctx, drsCtx, drsURI, cachePath, obj, accessURL)
}

func DownloadToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, cachePath string) error {
	if lfs.IsDRSURI(oid) {
		return DownloadDRSURIToCachePath(ctx, drsCtx, oid, cachePath)
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}

	accessURL, match, err := AccessURLForHashScope(ctx, drsCtx, oid)
	if err != nil {
		return err
	}
	return DownloadResolvedToCachePath(ctx, drsCtx, oid, cachePath, match, accessURL)
}

func DownloadResolvedToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, cachePath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}
	if obj == nil || accessURL == nil || accessURL.Url == "" {
		return DownloadToCachePath(ctx, drsCtx, oid, cachePath)
	}
	if isGlobusURL(accessURL.Url) {
		return downloadGlobusResolved(ctx, drsCtx, accessURL.Url, cachePath, oid, obj)
	}
	return downloadResolved(ctx, drsCtx, oid, cachePath, obj, accessURL)
}

func DownloadResolvedToPath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, dstPath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL, opts sydownload.DownloadOptions) error {
	if accessURL != nil && isGlobusURL(accessURL.Url) {
		return downloadGlobusResolved(ctx, drsCtx, accessURL.Url, dstPath, oid, obj)
	}
	if drsCtx == nil || drsCtx.Client == nil {
		return fmt.Errorf("DRS client unavailable")
	}
	if obj == nil || accessURL == nil || strings.TrimSpace(accessURL.Url) == "" {
		return fmt.Errorf("resolved DRS object and access URL are required")
	}
	src := &resolvedSource{
		requestor:    drsCtx.Client.Requestor(),
		accessURL:    strings.TrimSpace(accessURL.Url),
		headers:      accessURL.Headers,
		expectedSize: obj.Size,
	}
	return sydownload.DownloadToPathWithOptions(ctx, src, oid, dstPath, opts)
}

func downloadGlobusResolved(ctx context.Context, drsCtx *remoteruntime.GitContext, accessURL, dstPath, oid string, obj *drsapi.DrsObject) error {
	if obj == nil {
		return fmt.Errorf("resolved DRS object is required")
	}
	if err := transferGlobusToCachePath(ctx, drsCtx, accessURL, dstPath); err != nil {
		_ = os.Remove(dstPath)
		return err
	}
	if err := verifyGlobusDownload(dstPath, oid, obj, false); err != nil {
		_ = os.Remove(dstPath)
		return err
	}
	return nil
}

func verifyGlobusDownload(dstPath, oid string, obj *drsapi.DrsObject, placeholder bool) error {
	info, err := os.Stat(dstPath)
	if err != nil {
		return fmt.Errorf("verify Globus download: %w", err)
	}
	if info.Size() != obj.Size {
		return fmt.Errorf("verify Globus download: size mismatch: expected %d, got %d", obj.Size, info.Size())
	}
	want := ""
	if !placeholder {
		want = strings.ToLower(drsobject.NormalizeChecksum(oid))
		if decoded, err := hex.DecodeString(want); err != nil || len(decoded) != sha256.Size {
			want = ""
		}
	}
	if want == "" {
		for _, checksum := range obj.Checksums {
			checksumType := strings.ToLower(strings.TrimSpace(checksum.Type))
			if checksumType == "sha256" || checksumType == "sha-256" {
				want = strings.ToLower(drsobject.NormalizeChecksum(checksum.Checksum))
				break
			}
		}
	}
	if want == "" {
		return nil
	}
	f, err := os.Open(dstPath)
	if err != nil {
		return fmt.Errorf("verify Globus download: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("verify Globus download: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("verify Globus download: sha256 mismatch: expected %s, got %s", want, got)
	}
	return nil
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
	headers      *[]string
	expectedSize int64
}

type accessURLRequestor struct {
	request.Requester
	headers *[]string
}

func (r accessURLRequestor) Do(ctx context.Context, method, path string, body, out any, opts ...request.RequestOption) error {
	if r.headers != nil {
		for _, header := range *r.headers {
			key, value, ok := strings.Cut(header, ":")
			if !ok || strings.TrimSpace(key) == "" {
				return fmt.Errorf("invalid access URL header")
			}
			opts = append(opts, request.WithHeader(strings.TrimSpace(key), strings.TrimSpace(value)))
		}
	}
	return r.Requester.Do(ctx, method, path, body, out, opts...)
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
	resp, err := sytransfer.GenericDownload(ctx, accessURLRequestor{Requester: s.requestor, headers: s.headers}, s.accessURL, start, end)
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
