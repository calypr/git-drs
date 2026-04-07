package drs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/calypr/git-drs/client"
	localcommon "github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/syfon/client/conf"
	"github.com/calypr/syfon/client/drs"
	"github.com/calypr/syfon/client/pkg/common"
	"github.com/calypr/syfon/client/pkg/hash"
	"github.com/calypr/syfon/client/pkg/request"
	"github.com/calypr/syfon/client/transfer"
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
	API        drs.Client
	Credential *conf.Credential
	Logger     *slog.Logger
	Scope      pushScope
	Tuning     pushTuning
}

func newPushRuntime(cl *client.GitContext) *pushRuntime {
	if cl == nil {
		return &pushRuntime{}
	}
	return &pushRuntime{
		API:        cl.API,
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

func resolveRequestInterface(rt *pushRuntime) request.RequestInterface {
	if rt == nil {
		return nil
	}
	if req, ok := rt.API.(request.RequestInterface); ok {
		return req
	}
	return nil
}

// RegisterFile implements DRSClient.RegisterFile
// It registers (or reuses) an DRS object for the oid, uploads the object if it
// is not already available in the bucket, and returns the resulting DRS object.
func RegisterFile(cl *client.GitContext, ctx context.Context, oid string, path string) (*drs.DRSObject, error) {
	rt := newPushRuntime(cl)
	oid = drs.NormalizeOid(oid)
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
func isFileDownloadable(rt *pushRuntime, ctx context.Context, drsObject *drs.DRSObject) (bool, error) {
	// Try to get a download URL - if successful, file is downloadable
	if len(drsObject.AccessMethods) == 0 {
		return false, nil
	}
	accessType := drsObject.AccessMethods[0].Type
	res, err := rt.API.GetDownloadURL(ctx, drsObject.Id, accessType)
	if err != nil {
		// If we can't get a download URL, assume file is not downloadable
		return false, nil
	}
	// Check if the URL is accessible
	err = common.CanDownloadFile(res.Url)
	return err == nil, nil
}

func uploadKeyFromObject(obj *drs.DRSObject, bucket string) string {
	if obj != nil && len(obj.AccessMethods) > 0 {
		raw := strings.TrimSpace(obj.AccessMethods[0].AccessUrl.Url)
		if raw != "" {
			if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Scheme, "s3") {
				// Preserve the full object key path from DRS metadata.
				// Taking only filepath.Base(...) loses CAS/storage prefixes and causes 404 downloads.
				key := strings.TrimSpace(strings.TrimPrefix(u.Path, "/"))
				if key != "" && (bucket == "" || strings.EqualFold(strings.TrimSpace(u.Host), strings.TrimSpace(bucket))) {
					return key
				}
			}
		}
	}
	if obj != nil {
		return hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256
	}
	return ""
}

func resolveUploadSourcePath(oid string, worktreePath string, isPointer bool) (string, bool, error) {
	oid = drs.NormalizeOid(oid)
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

func ensureRecordRegistered(rt *pushRuntime, ctx context.Context, oid string, path string) (*drs.DRSObject, error) {
	drsObject, err := drsmap.DrsInfoFromOid(oid)
	if err != nil {
		stat, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("error reading local record for oid %s: %v (also failed to stat file %s: %v)", oid, err, path, statErr)
		}
		drsId := drsmap.DrsUUID(rt.Scope.ProjectID, oid)
		drsObject, err = BuildDrsObj(filepath.Base(path), oid, stat.Size(), drsId, rt.Scope.Bucket, rt.Scope.Organization, rt.Scope.ProjectID, rt.Scope.StoragePref)
		if err != nil {
			return nil, fmt.Errorf("error building drs info for oid %s: %v", oid, err)
		}
	}
	rt.Logger.InfoContext(ctx, fmt.Sprintf("registering record for oid %s in DRS object (did: %s)", oid, drsObject.Id))
	registeredObjs, err := rt.API.RegisterRecords(ctx, []*drs.DRSObject{drsObject})
	var registeredObj *drs.DRSObject
	if len(registeredObjs) > 0 {
		registeredObj = registeredObjs[0]
	}
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			if !rt.Tuning.Upsert {
				rt.Logger.DebugContext(ctx, fmt.Sprintf("DRS object already exists, proceeding for oid %s: did: %s err: %v", oid, drsObject.Id, err))
				if recs, lookupErr := GetObjectByHashForGit(ctx, rt.API, oid, rt.Scope.Organization, rt.Scope.ProjectID); lookupErr == nil && len(recs) > 0 {
					if match, matchErr := drsmap.FindMatchingRecord(recs, rt.Scope.Organization, rt.Scope.ProjectID); matchErr == nil && match != nil {
						drsObject = match
					}
				}
			} else {
				rt.Logger.DebugContext(ctx, fmt.Sprintf("DRS object already exists, deleting and re-adding for oid %s: did: %s", oid, drsObject.Id))
				err = DeleteRecordsByOID(ctx, rt.API, oid)
				if err != nil {
					return nil, fmt.Errorf("error deleting existing DRS object oid %s: did: %s err: %v", oid, drsObject.Id, err)
				}
				registeredObjs, err = rt.API.RegisterRecords(ctx, []*drs.DRSObject{drsObject})
				if err != nil {
					return nil, fmt.Errorf("error re-saving DRS object after deletion: oid %s: did: %s err: %v", oid, drsObject.Id, err)
				}
				if len(registeredObjs) > 0 {
					registeredObj = registeredObjs[0]
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

func uploadFileForObject(rt *pushRuntime, ctx context.Context, drsObject *drs.DRSObject, filePath string, skipIfDownloadable bool, presignedURL string) error {
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
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening file %s: %v", filePath, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			rt.Logger.DebugContext(ctx, fmt.Sprintf("warning: error closing file %s: %v", filePath, err))
		}
	}(file)

	multiPartThreshold := int64(5 * 1024 * 1024 * 1024)
	if rt.Tuning.MultiPartThreshold > 0 {
		multiPartThreshold = rt.Tuning.MultiPartThreshold
	}
	fileStat, statErr := file.Stat()
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
		req := resolveRequestInterface(rt)
		if req == nil {
			return fmt.Errorf("presigned upload requested but request interface is unavailable")
		}
		if _, err := transfer.DoUpload(ctx, req, presignedURL, file, fileSize); err != nil {
			return fmt.Errorf("presigned upload failed: %w", err)
		}
		return nil
	}

	objectKey := uploadKeyFromObject(drsObject, rt.Scope.Bucket)
	uploader, ok := rt.API.(transfer.Uploader)
	if !ok {
		return fmt.Errorf("drs API does not implement transfer.Uploader")
	}
	rt.Logger.DebugContext(ctx, "uploading via data-client orchestrator",
		"size", fileSize,
		"path", filePath,
		"threshold", multiPartThreshold,
	)
	if err := upload.Upload(ctx, uploader, common.FileUploadRequestObject{
		GUID:       drsObject.Id,
		ObjectKey:  objectKey,
		SourcePath: filePath,
	}, false); err != nil {
		return fmt.Errorf("upload error: %w", err)
	}
	return nil
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

func getSHA256ValidityMap(cl *client.GitContext, ctx context.Context, oids []string) (map[string]bool, error) {
	return getSHA256ValidityMapRuntime(newPushRuntime(cl), ctx, oids)
}

func getSHA256ValidityMapRuntime(rt *pushRuntime, ctx context.Context, oids []string) (map[string]bool, error) {
	if rt == nil || rt.Credential == nil {
		return nil, fmt.Errorf("gen3 context not available for validity check")
	}
	cred := rt.Credential
	if strings.TrimSpace(cred.APIEndpoint) == "" {
		return nil, fmt.Errorf("missing API endpoint for validity check")
	}

	reqBody, err := json.Marshal(map[string][]string{"sha256": oids})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cred.APIEndpoint, "/")+"/index/bulk/sha256/validity", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(cred.AccessToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("validity endpoint status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
