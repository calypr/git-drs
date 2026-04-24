package pushsync

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	localcommon "github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drsmap"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/conf"
	"github.com/calypr/syfon/client/drsmeta"
	"github.com/calypr/syfon/client/hash"
	syrequest "github.com/calypr/syfon/client/request"
	"github.com/calypr/syfon/client/xfer/upload"
)

type pushScope struct {
	Organization string
	ProjectID    string
	Bucket       string
	StoragePref  string
}

type pushTuning struct {
	Upsert             bool
	MultiPartThreshold int64
	UploadConcurrency  int
}

type pushRuntime struct {
	API        *config.GitContext
	Credential *conf.Credential
	Logger     *slog.Logger
	Scope      pushScope
	Tuning     pushTuning
}

func newPushRuntime(cl *config.GitContext) *pushRuntime {
	if cl == nil {
		return &pushRuntime{}
	}
	return &pushRuntime{
		API:        cl,
		Credential: cl.Credential,
		Logger:     cl.Logger,
		Scope: pushScope{
			Organization: cl.Organization,
			ProjectID:    cl.ProjectId,
			Bucket:       cl.BucketName,
			StoragePref:  cl.StoragePrefix,
		},
		Tuning: pushTuning{
			Upsert:             cl.Upsert,
			MultiPartThreshold: cl.MultiPartThreshold,
			UploadConcurrency:  cl.UploadConcurrency,
		},
	}
}

// RegisterFile implements DRSClient.RegisterFile
// It registers (or reuses) an DRS object for the oid, uploads the object if it
// is not already available in the bucket, and returns the resulting DRS object.
func RegisterFile(cl *config.GitContext, ctx context.Context, oid string, path string) (*drsapi.DrsObject, error) {
	rt := newPushRuntime(cl)
	oid = localcommon.NormalizeOid(oid)
	rt.Logger.DebugContext(ctx, fmt.Sprintf("register file started for oid: %s", oid))

	drsObject, err := ensureRecordRegistered(rt, ctx, oid, path)
	if err != nil {
		return nil, err
	}
	if err := uploadFileForObject(rt, ctx, drsObject, path, true, ""); err != nil {
		return nil, err
	}
	return drsObject, nil
}

// isFileDownloadable checks if a file is already available for download
func isFileDownloadable(rt *pushRuntime, ctx context.Context, drsObject *drsapi.DrsObject) (bool, error) {
	// Try to get a download URL - if successful, file is downloadable
	if drsObject.AccessMethods == nil || len(*drsObject.AccessMethods) == 0 {
		return false, nil
	}
	accessType := (*drsObject.AccessMethods)[0].Type
	res, err := rt.API.Client.DRS().GetAccessURL(ctx, drsObject.Id, string(accessType))
	if err != nil {
		// If we can't get a download URL, assume file is not downloadable
		return false, nil
	}
	// Check if the URL is accessible
	err = common.CanDownloadFile(res.Url)
	return err == nil, nil
}

func uploadKeyFromObject(obj *drsapi.DrsObject, bucket string, storagePrefix string) string {
	prefix := strings.Trim(strings.TrimSpace(storagePrefix), "/")
	applyPrefix := func(key string) string {
		key = strings.Trim(strings.TrimSpace(key), "/")
		if key == "" {
			return ""
		}
		if prefix == "" || key == prefix || strings.HasPrefix(key, prefix+"/") {
			return key
		}
		return prefix + "/" + key
	}

	if obj != nil && obj.AccessMethods != nil && len(*obj.AccessMethods) > 0 {
		raw := ""
		if (*obj.AccessMethods)[0].AccessUrl != nil {
			raw = strings.TrimSpace((*obj.AccessMethods)[0].AccessUrl.Url)
		}
		if raw != "" {
			if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Scheme, "s3") {
				// Preserve the full object key path from DRS metadata.
				// Taking only filepath.Base(...) loses CAS/storage prefixes and causes 404 downloads.
				key := strings.TrimSpace(strings.TrimPrefix(u.Path, "/"))
				if key != "" && (bucket == "" || strings.EqualFold(strings.TrimSpace(u.Host), strings.TrimSpace(bucket))) {
					return applyPrefix(key)
				}
			}
		}
	}
	if obj != nil {
		return applyPrefix(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256)
	}
	return ""
}

func resolveUploadSourcePath(oid string, worktreePath string, isPointer bool) (string, bool, error) {
	oid = localcommon.NormalizeOid(oid)
	if oid == "" {
		return "", false, fmt.Errorf("empty oid")
	}

	lfsObjPath, err := lfs.ObjectPath(localcommon.LFS_OBJS_PATH, oid)
	if err == nil {
		if st, statErr := os.Stat(lfsObjPath); statErr == nil && !st.IsDir() && st.Size() > 0 {
			if isPointer {
				if sentinel, sentinelErr := lfs.IsAddURLSentinelObject(lfsObjPath); sentinelErr == nil && sentinel {
					return "", false, nil
				}
			}
			return lfsObjPath, true, nil
		}
	}

	if isPointer {
		return "", false, nil
	}

	st, statErr := os.Stat(worktreePath)
	if statErr != nil {
		return "", false, fmt.Errorf("stat worktree path %s: %w", worktreePath, statErr)
	}
	if st.IsDir() {
		return "", false, fmt.Errorf("worktree path %s is a directory", worktreePath)
	}
	return worktreePath, true, nil
}

func ensureRecordRegistered(rt *pushRuntime, ctx context.Context, oid string, path string) (*drsapi.DrsObject, error) {
	drsObject, err := drsmap.DrsInfoFromOid(oid)
	if err != nil {
		stat, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("error reading local record for oid %s: %v (also failed to stat file %s: %v)", oid, err, path, statErr)
		}
		drsId := drsmap.DrsUUID(rt.Scope.ProjectID, oid)
		drsObject, err = drsmeta.BuildDrsObjWithPrefix(filepath.Base(path), oid, stat.Size(), drsId, rt.Scope.Bucket, rt.Scope.Organization, rt.Scope.ProjectID, rt.Scope.StoragePref)
		if err != nil {
			return nil, fmt.Errorf("error building drs info for oid %s: %v", oid, err)
		}
	}
	rt.Logger.InfoContext(ctx, fmt.Sprintf("registering record for oid %s in DRS object (did: %s)", oid, drsObject.Id))
	registeredObjs, err := rt.API.Client.DRS().RegisterObjects(ctx, drsapi.RegisterObjectsJSONRequestBody{
		Candidates: []drsapi.DrsObjectCandidate{drsmeta.ConvertToCandidate(drsObject)},
	})
	var registeredObj *drsapi.DrsObject
	if len(registeredObjs.Objects) > 0 {
		registeredObj = &registeredObjs.Objects[0]
	}
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			if !rt.Tuning.Upsert {
				rt.Logger.DebugContext(ctx, fmt.Sprintf("DRS object already exists, proceeding for oid %s: did: %s err: %v", oid, drsObject.Id, err))
				if recs, lookupErr := rt.API.Client.DRS().GetObjectsByHashForResource(ctx, oid, rt.Scope.Organization, rt.Scope.ProjectID); lookupErr == nil && len(recs) > 0 {
					if match, matchErr := drsmap.FindMatchingRecord(recs, rt.Scope.Organization, rt.Scope.ProjectID); matchErr == nil && match != nil {
						drsObject = match
					}
				}
			} else {
				rt.Logger.DebugContext(ctx, fmt.Sprintf("DRS object already exists, deleting and re-adding for oid %s: did: %s", oid, drsObject.Id))
				err = rt.API.Client.DRS().DeleteRecordsByHash(ctx, oid)
				if err != nil {
					return nil, fmt.Errorf("error deleting existing DRS object oid %s: did: %s err: %v", oid, drsObject.Id, err)
				}
				registeredObjs, err = rt.API.Client.DRS().RegisterObjects(ctx, drsapi.RegisterObjectsJSONRequestBody{
					Candidates: []drsapi.DrsObjectCandidate{drsmeta.ConvertToCandidate(drsObject)},
				})
				if err != nil {
					return nil, fmt.Errorf("error re-saving DRS object after deletion: oid %s: did: %s err: %v", oid, drsObject.Id, err)
				}
				if len(registeredObjs.Objects) > 0 {
					registeredObj = &registeredObjs.Objects[0]
				}
				if registeredObj != nil {
					drsObject = registeredObj
				}
			}
		} else {
			return nil, fmt.Errorf("error saving oid %s DRS object: %v", oid, err)
		}
	} else if registeredObj != nil {
		drsObject = registeredObj
	}
	rt.Logger.InfoContext(ctx, fmt.Sprintf("DRS object registration complete for oid %s", oid))
	return drsObject, nil
}

func uploadFileForObject(rt *pushRuntime, ctx context.Context, drsObject *drsapi.DrsObject, filePath string, skipIfDownloadable bool, presignedURL string) error {
	hInfo := hash.ConvertDrsChecksumsToHashInfo(drsObject.Checksums)
	if skipIfDownloadable {
		rt.Logger.InfoContext(ctx, fmt.Sprintf("checking if oid %s is already downloadable", hInfo.SHA256))
		downloadable, err := isFileDownloadable(rt, ctx, drsObject)
		if err != nil {
			return fmt.Errorf("error checking if file is downloadable: oid %s %v", hInfo.SHA256, err)
		}
		if downloadable {
			rt.Logger.DebugContext(ctx, fmt.Sprintf("file %s is already available for download, skipping upload", hInfo.SHA256))
			return nil
		}
	}

	rt.Logger.InfoContext(ctx, fmt.Sprintf("file %s is not downloadable, proceeding to upload", hInfo.SHA256))
	multiPartThreshold := int64(5 * 1024 * 1024 * 1024)
	if rt.Tuning.MultiPartThreshold > 0 {
		multiPartThreshold = rt.Tuning.MultiPartThreshold
	}
	fileStat, statErr := os.Stat(filePath)
	if statErr != nil {
		return fmt.Errorf("error stat file %s: %v", filePath, statErr)
	}
	fileSize := fileStat.Size()
	drsSize := drsObject.Size
	if drsSize != fileSize {
		rt.Logger.WarnContext(ctx, "drs metadata size differs from local source size; using local file size for upload mode decision",
			"did", drsObject.Id,
			"path", filePath,
			"drs_size", drsSize,
			"file_size", fileSize,
		)
	}

	if strings.TrimSpace(presignedURL) != "" {
		rt.Logger.DebugContext(ctx, "uploading via presigned URL",
			"did", drsObject.Id,
			"path", filePath,
			"size", fileSize,
		)
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("error opening file %s: %v", filePath, err)
		}
		defer file.Close()
		if err := doPresignedUpload(ctx, rt.API.Requestor, presignedURL, file, fileSize); err != nil {
			return fmt.Errorf("presigned upload failed: %w", err)
		}
		return nil
	}

	objectKey := uploadKeyFromObject(drsObject, rt.Scope.Bucket, rt.Scope.StoragePref)
	uploader := rt.API.Client.Data()
	if uploader == nil {
		return fmt.Errorf("drs API does not expose upload capability")
	}
	rt.Logger.DebugContext(ctx, "uploading via data-client orchestrator",
		"size", fileSize,
		"path", filePath,
		"threshold", multiPartThreshold,
	)
	forceMultipart := fileSize >= multiPartThreshold
	rt.Logger.DebugContext(ctx, "uploading via syfon transfer engine",
		"did", drsObject.Id,
		"size", fileSize,
		"threshold", multiPartThreshold,
		"forceMultipart", forceMultipart,
	)
	if err := upload.Upload(ctx, uploader, filePath, objectKey, drsObject.Id, rt.Scope.Bucket, common.FileMetadata{}, false, forceMultipart); err != nil {
		return fmt.Errorf("upload error: %w", err)
	}
	return nil
}

func doPresignedUpload(ctx context.Context, req syrequest.Requester, urlStr string, body io.Reader, size int64) error {
	parsed, err := url.Parse(strings.TrimSpace(urlStr))
	if err != nil {
		return fmt.Errorf("parse presigned upload url: %w", err)
	}

	method := http.MethodPut
	if useGCSJSONMediaUpload(parsed) {
		method = http.MethodPost
	}

	var resp *http.Response
	var opts []syrequest.RequestOption
	if size > 0 {
		opts = append(opts, syrequest.WithPartSize(size))
	}
	if common.IsCloudPresignedURL(urlStr) {
		opts = append(opts, syrequest.WithSkipAuth(true))
	}
	if method == http.MethodPut && needsAzureBlobTypeHeader(parsed) {
		opts = append(opts, syrequest.WithHeader("x-ms-blob-type", "BlockBlob"))
	}

	if err := req.Do(ctx, method, urlStr, body, &resp, opts...); err != nil {
		return fmt.Errorf("upload to %s failed: %w", urlStr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return common.ResponseBodyError(resp, fmt.Sprintf("upload to %s failed", urlStr))
	}
	return nil
}

func needsAzureBlobTypeHeader(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return false
	}
	q := parsed.Query()
	if strings.TrimSpace(q.Get("comp")) != "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(q.Get("sr")), "b") {
		return false
	}
	return strings.TrimSpace(q.Get("sig")) != "" && strings.TrimSpace(q.Get("sv")) != ""
}

func useGCSJSONMediaUpload(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	if strings.TrimSpace(parsed.Query().Get("uploadType")) != "media" {
		return false
	}
	if strings.TrimSpace(parsed.Query().Get("name")) == "" {
		return false
	}
	return strings.Contains(parsed.EscapedPath(), "/upload/storage/v1/b/")
}

func uploadChunkSizeForThreshold(threshold int64) int64 {
	chunkSize := int64(64 * common.MB)
	if threshold > 0 && threshold < chunkSize {
		chunkSize = threshold
	}
	if chunkSize < common.MinMultipartChunkSize {
		chunkSize = common.MinMultipartChunkSize
	}
	return chunkSize
}

func getSHA256ValidityMap(cl *config.GitContext, ctx context.Context, oids []string) (map[string]bool, error) {
	return getSHA256ValidityMapRuntime(newPushRuntime(cl), ctx, oids)
}

func getSHA256ValidityMapRuntime(rt *pushRuntime, ctx context.Context, oids []string) (map[string]bool, error) {
	if rt == nil || rt.API == nil || rt.API.Requestor == nil {
		return nil, fmt.Errorf("DRS client not available for validity check")
	}
	var out map[string]bool
	if err := rt.API.Requestor.Do(ctx, http.MethodPost, "/index/bulk/sha256/validity", map[string][]string{"sha256": oids}, &out); err != nil {
		return nil, fmt.Errorf("validity check: %w", err)
	}
	return out, nil
}
