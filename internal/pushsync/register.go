package pushsync

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	localcommon "github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	conf "github.com/calypr/syfon/client/config"
	"github.com/calypr/syfon/client/hash"
	syupload "github.com/calypr/syfon/client/transfer/upload"
)

type pushScope struct {
	Organization string
	Project      string
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
	ProbeURL   func(context.Context, string) error
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
			Project:      cl.ProjectId,
			Bucket:       cl.BucketName,
			StoragePref:  cl.StoragePrefix,
		},
		Tuning: pushTuning{
			Upsert:             cl.Upsert,
			MultiPartThreshold: cl.MultiPartThreshold,
			UploadConcurrency:  cl.UploadConcurrency,
		},
		ProbeURL: newDownloadProbe(cl),
	}
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
	if rt.ProbeURL == nil {
		rt.ProbeURL = newDownloadProbe(rt.API)
	}
	err = rt.ProbeURL(ctx, res.Url)
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
					return key
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
	oid = localdrsobject.NormalizeOid(oid)
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

func uploadFileForObject(rt *pushRuntime, ctx context.Context, drsObject *drsapi.DrsObject, filePath string, skipIfDownloadable bool) error {
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

	objectKey := uploadKeyFromObject(drsObject, rt.Scope.Bucket, rt.Scope.StoragePref)
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
	if err := syupload.UploadObjectFile(ctx, rt.API.Client.Data(), filePath, objectKey, drsObject.Id, rt.Scope.Bucket, forceMultipart); err != nil {
		return fmt.Errorf("upload error: %w", err)
	}
	return nil
}

func newDownloadProbe(cl *config.GitContext) func(context.Context, string) error {
	httpClient := http.DefaultClient
	if cl != nil && cl.Client != nil && cl.Client.HTTPClient() != nil {
		httpClient = cl.Client.HTTPClient()
	}
	return func(ctx context.Context, rawURL string) error {
		return probeDownloadURL(ctx, httpClient, rawURL)
	}
}

func probeDownloadURL(ctx context.Context, httpClient *http.Client, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download probe failed with status %d", resp.StatusCode)
	}
	return nil
}
