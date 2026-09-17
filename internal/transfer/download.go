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
	"sync"

	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/hash"
	"github.com/calypr/syfon/client/request"
	sytransfer "github.com/calypr/syfon/client/transfer"
	sydownload "github.com/calypr/syfon/client/transfer/download"
)

func AccessURLForHashScope(ctx context.Context, drsCtx *remoteruntime.GitContext, checksum string) (*drsapi.AccessURL, *drsapi.DrsObject, error) {
	resolved, object, err := ResolvedAccessURLForHashScope(ctx, drsCtx, checksum)
	if err != nil {
		return nil, nil, err
	}
	return &resolved.AccessURL, object, nil
}

func ResolvedAccessURLForHashScope(ctx context.Context, drsCtx *remoteruntime.GitContext, checksum string) (ResolvedAccess, *drsapi.DrsObject, error) {
	records, err := lookup.ObjectsByHashForScope(ctx, drsCtx, checksum)
	if err != nil {
		return ResolvedAccess{}, nil, err
	}
	if len(records) == 0 {
		return ResolvedAccess{}, nil, fmt.Errorf("no matching DRS record found for oid %s", hash.NormalizeChecksum(checksum))
	}
	match := records[0]
	if match.AccessMethods == nil || len(*match.AccessMethods) == 0 {
		return ResolvedAccess{}, nil, fmt.Errorf("AccessURLForHashScope: no access methods *available* for DRS object %s", match.Id)
	}
	access, err := planResolvedAccess(ctx, drsCtx, match)
	if err != nil {
		return ResolvedAccess{}, nil, err
	}
	return access, &match, nil
}

func AccessURLForDRSURI(ctx context.Context, drsCtx *remoteruntime.GitContext, drsURI string) (*drsapi.AccessURL, *drsapi.DrsObject, error) {
	access, obj, err := ResolvedAccessURLForDRSURI(ctx, drsCtx, drsURI)
	if err != nil {
		return nil, nil, err
	}
	return &access.AccessURL, obj, nil
}

func ResolvedAccessURLForDRSURI(ctx context.Context, drsCtx *remoteruntime.GitContext, drsURI string) (ResolvedAccess, *drsapi.DrsObject, error) {
	if strings.HasPrefix(drsURI, "//") {
		drsURI = "drs:" + drsURI
	}
	obj, err := drsCtx.Client.DRS().GetObject(ctx, drsURI)
	if err != nil {
		return ResolvedAccess{}, nil, err
	}
	if obj.AccessMethods == nil || len(*obj.AccessMethods) == 0 {
		return ResolvedAccess{}, nil, fmt.Errorf("AccessURLForDRSURI: no access methods *available* for DRS object %s", obj.Id)
	}
	access, err := planResolvedAccess(ctx, drsCtx, obj)
	if err != nil {
		return ResolvedAccess{}, nil, err
	}
	return access, &obj, nil
}

func DownloadDRSURIToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, drsURI, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}
	access, obj, err := ResolvedAccessURLForDRSURI(ctx, drsCtx, drsURI)
	if err != nil {
		return err
	}
	return DownloadResolvedToCachePathWithAccess(ctx, drsCtx, drsURI, cachePath, obj, access)
}

func DownloadToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, cachePath string) error {
	if lfs.IsDRSURI(oid) {
		return DownloadDRSURIToCachePath(ctx, drsCtx, oid, cachePath)
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}

	access, match, err := ResolvedAccessURLForHashScope(ctx, drsCtx, oid)
	if err != nil {
		return err
	}
	return DownloadResolvedToCachePathWithAccess(ctx, drsCtx, oid, cachePath, match, access)
}

// DownloadToPath resolves a DRS object by checksum and writes its payload to
// the requested destination. Unlike DownloadToCachePath, this does not use
// the Git-LFS object cache and is intended for callers that are not operating
// on a Git checkout.
func DownloadToPath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, dstPath string) error {
	access, match, err := ResolvedAccessURLForHashScope(ctx, drsCtx, oid)
	if err != nil {
		return err
	}
	return DownloadResolvedToPathWithAccess(ctx, drsCtx, oid, dstPath, match, access, sydownload.DownloadOptions{
		MultipartThreshold: 5 * 1024 * 1024,
		Concurrency:        2,
		ChunkSize:          64 * 1024 * 1024,
	})
}

func DownloadResolvedToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, cachePath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL) error {
	access := compatibilityResolvedAccess(obj, accessURL)
	return DownloadResolvedToCachePathWithAccess(ctx, drsCtx, oid, cachePath, obj, access)
}

func DownloadResolvedToCachePathWithAccess(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, cachePath string, obj *drsapi.DrsObject, access ResolvedAccess, cacheRoots ...string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}
	if obj == nil || strings.TrimSpace(access.AccessURL.Url) == "" {
		return DownloadToCachePath(ctx, drsCtx, oid, cachePath)
	}
	cacheRoots = resolvedCacheRoots(drsCtx, cacheRoots)
	if isGlobusURL(access.AccessURL.Url) {
		return downloadGlobusResolved(ctx, drsCtx, access.AccessURL.Url, cachePath, oid, obj, cacheRoots...)
	}
	return DownloadResolvedToPathWithAccess(ctx, drsCtx, oid, cachePath, obj, access, sydownload.DownloadOptions{
		MultipartThreshold: 5 * 1024 * 1024,
		Concurrency:        2,
		ChunkSize:          64 * 1024 * 1024,
	})
}

func DownloadResolvedToPath(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, dstPath string, obj *drsapi.DrsObject, accessURL *drsapi.AccessURL, opts sydownload.DownloadOptions) error {
	access := compatibilityResolvedAccess(obj, accessURL)
	return DownloadResolvedToPathWithAccess(ctx, drsCtx, oid, dstPath, obj, access, opts)
}

// compatibilityResolvedAccess preserves refresh support for callers of the
// legacy URL-only helpers when the access method is unambiguous. Production
// planning paths carry ResolvedAccess directly and never infer this identity.
func compatibilityResolvedAccess(obj *drsapi.DrsObject, accessURL *drsapi.AccessURL) ResolvedAccess {
	var resolved ResolvedAccess
	if accessURL == nil {
		return resolved
	}
	resolved.AccessURL = cloneAccessURL(*accessURL, strings.TrimSpace(accessURL.Url))
	if obj == nil || obj.AccessMethods == nil {
		return resolved
	}

	var onlyAccessID string
	for _, method := range *obj.AccessMethods {
		if method.AccessId == nil || strings.TrimSpace(*method.AccessId) == "" {
			continue
		}
		accessID := strings.TrimSpace(*method.AccessId)
		if method.AccessUrl != nil && strings.TrimSpace(method.AccessUrl.Url) == resolved.AccessURL.Url {
			resolved.AccessID = accessID
			return resolved
		}
		if onlyAccessID == "" {
			onlyAccessID = accessID
		} else if onlyAccessID != accessID {
			return resolved
		}
	}
	resolved.AccessID = onlyAccessID
	return resolved
}

func DownloadResolvedToPathWithAccess(ctx context.Context, drsCtx *remoteruntime.GitContext, oid, dstPath string, obj *drsapi.DrsObject, access ResolvedAccess, opts sydownload.DownloadOptions, cacheRoots ...string) error {
	cacheRoots = resolvedCacheRoots(drsCtx, cacheRoots)
	if strings.TrimSpace(access.AccessURL.Url) != "" && isGlobusURL(access.AccessURL.Url) {
		return downloadGlobusResolved(ctx, drsCtx, access.AccessURL.Url, dstPath, oid, obj, cacheRoots...)
	}
	if drsCtx == nil || drsCtx.Client == nil {
		return fmt.Errorf("DRS client unavailable")
	}
	if obj == nil || strings.TrimSpace(access.AccessURL.Url) == "" {
		return fmt.Errorf("resolved DRS object and access URL are required")
	}
	_, statErr := os.Lstat(dstPath)
	if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("stat download destination: %w", statErr)
	}
	hadDestination := statErr == nil
	src := &resolvedSource{
		requestor:    drsCtx.Client,
		accessURL:    strings.TrimSpace(access.AccessURL.Url),
		headers:      cloneAccessHeaders(access.AccessURL.Headers),
		accessID:     strings.TrimSpace(access.AccessID),
		expectedSize: obj.Size,
		identity:     resolvedDownloadIdentity(oid, obj),
	}
	if src.accessID != "" && strings.TrimSpace(obj.Id) != "" {
		src.drsClient = drsCtx.Client.DRS()
		src.objectID = strings.TrimSpace(obj.Id)
	}
	err := sydownload.DownloadToPathWithOptions(ctx, src, oid, dstPath, opts)
	if err != nil && !hadDestination {
		// The transfer engine creates its resume checkpoint before the first
		// request. Do not leave a failed new download looking resumable to a
		// caller; an existing partial destination remains available for retry.
		_ = os.Remove(dstPath)
		_ = os.Remove(dstPath + ".syfon-download.json")
	}
	return err
}

func resolvedCacheRoots(drsCtx *remoteruntime.GitContext, roots []string) []string {
	if len(roots) > 0 || drsCtx == nil || strings.TrimSpace(drsCtx.LFSObjectsRoot) == "" || strings.TrimSpace(drsCtx.RepositoryRoot) == "" {
		return roots
	}
	return []string{drsCtx.LFSObjectsRoot, drsCtx.RepositoryRoot}
}

func resolvedDownloadIdentity(oid string, obj *drsapi.DrsObject) string {
	for _, checksum := range obj.Checksums {
		if strings.EqualFold(strings.TrimSpace(checksum.Type), "sha256") {
			if value := strings.ToLower(strings.TrimSpace(checksum.Checksum)); value != "" {
				return "sha256:" + value
			}
		}
	}
	return strings.TrimSpace(oid)
}

func downloadGlobusResolved(ctx context.Context, drsCtx *remoteruntime.GitContext, accessURL, dstPath, oid string, obj *drsapi.DrsObject, cacheRoots ...string) error {
	if obj == nil {
		return fmt.Errorf("resolved DRS object is required")
	}
	if err := transferGlobusToCachePath(ctx, drsCtx, accessURL, dstPath, cacheRoots...); err != nil {
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
	placeholder = placeholder || hasPlaceholderChecksum(obj, oid)
	want := ""
	if !placeholder {
		want = strings.ToLower(hash.NormalizeChecksum(oid))
		if decoded, err := hex.DecodeString(want); err != nil || len(decoded) != sha256.Size {
			want = ""
		}
	}
	if want == "" {
		for _, checksum := range obj.Checksums {
			checksumType := strings.ToLower(strings.TrimSpace(checksum.Type))
			if checksumType == "sha256" || checksumType == "sha-256" {
				want = strings.ToLower(hash.NormalizeChecksum(checksum.Checksum))
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

func hasPlaceholderChecksum(obj *drsapi.DrsObject, oid string) bool {
	if obj == nil {
		return false
	}
	for _, checksum := range obj.Checksums {
		if strings.EqualFold(strings.TrimSpace(checksum.Type), "git-drs-placeholder") && strings.EqualFold(hash.NormalizeChecksum(checksum.Checksum), hash.NormalizeChecksum(oid)) {
			return true
		}
	}
	return false
}

type resolvedSource struct {
	requestor interface {
		Do(*http.Request) (*http.Response, error)
	}
	accessURLMu sync.RWMutex
	accessURL   string
	headers     *[]string
	drsClient   interface {
		GetAccessURL(context.Context, string, string) (drsapi.AccessURL, error)
	}
	objectID     string
	accessID     string
	expectedSize int64
	identity     string
}

type accessURLRequestor struct {
	request.HTTPDoer
	headers *[]string
}

func (r accessURLRequestor) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("download request is nil")
	}
	requestCopy := req.Clone(req.Context())
	if r.headers != nil {
		for _, header := range *r.headers {
			key, value, ok := strings.Cut(header, ":")
			if !ok || strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("invalid access URL header")
			}
			requestCopy.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
		}
	}
	return r.HTTPDoer.Do(requestCopy)
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
		Identity:     s.identity,
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
	accessURL, headers := s.currentAccess()
	requestor := accessURLRequestor{HTTPDoer: s.requestor, headers: headers}
	resp, err := sytransfer.GenericDownload(ctx, requestor, accessURL, start, end)
	if err != nil {
		return nil, err
	}
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && s.canRefreshAccessURL() {
		resp.Body.Close()
		accessURL, headers, err = s.refreshAccessURL(ctx, accessURL)
		if err != nil {
			return nil, fmt.Errorf("refresh DRS access URL: %w", err)
		}
		requestor = accessURLRequestor{HTTPDoer: s.requestor, headers: headers}
		resp, err = sytransfer.GenericDownload(ctx, requestor, accessURL, start, end)
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		err := sycommon.ResponseBodyError(resp, fmt.Sprintf("download from %s failed", accessURL))
		resp.Body.Close()
		if permanentDownloadStatus(resp.StatusCode) {
			return nil, sytransfer.NonRetryable(err)
		}
		return nil, err
	}
	if start != nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil, sytransfer.ErrRangeIgnored
	}
	return resp.Body, nil
}

func (s *resolvedSource) canRefreshAccessURL() bool {
	return s.drsClient != nil && s.objectID != "" && s.accessID != ""
}

func (s *resolvedSource) refreshAccessURL(ctx context.Context, failedURL string) (string, *[]string, error) {
	s.accessURLMu.Lock()
	defer s.accessURLMu.Unlock()
	if s.accessURL != failedURL {
		return s.accessURL, cloneAccessHeaders(s.headers), nil
	}
	refreshed, err := s.drsClient.GetAccessURL(ctx, s.objectID, s.accessID)
	if err != nil {
		return "", nil, err
	}
	refreshedURL := strings.TrimSpace(refreshed.Url)
	if refreshedURL == "" {
		return "", nil, fmt.Errorf("DRS access URL is empty")
	}
	s.accessURL = refreshedURL
	s.headers = cloneAccessHeaders(refreshed.Headers)
	return s.accessURL, cloneAccessHeaders(s.headers), nil
}

func (s *resolvedSource) currentAccess() (string, *[]string) {
	s.accessURLMu.RLock()
	defer s.accessURLMu.RUnlock()
	return s.accessURL, cloneAccessHeaders(s.headers)
}

func permanentDownloadStatus(status int) bool {
	return status >= http.StatusBadRequest && status < http.StatusInternalServerError &&
		status != http.StatusRequestTimeout && status != http.StatusTooManyRequests
}
