package transfer

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
	syaccess "github.com/calypr/syfon/client/access"
	sycommon "github.com/calypr/syfon/client/common"
	conf "github.com/calypr/syfon/client/config"
	"github.com/calypr/syfon/client/hash"
	sytransfer "github.com/calypr/syfon/client/transfer"
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
	ForceUpload        bool
	MultiPartThreshold int64
	UploadConcurrency  int
}

type pushRuntime struct {
	API         *remoteruntime.GitContext
	Backend     sytransfer.MultipartBackend
	Credential  *conf.Credential
	Logger      *slog.Logger
	Scope       pushScope
	Tuning      pushTuning
	ObjectsRoot string
}

func newPushRuntime(cl *remoteruntime.GitContext) *pushRuntime {
	if cl == nil {
		return &pushRuntime{}
	}
	var backend sytransfer.MultipartBackend
	if cl.Client != nil {
		backend = cl.Client.Data()
	}
	return &pushRuntime{
		API:        cl,
		Backend:    backend,
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
			ForceUpload:        cl.ForceUpload,
			MultiPartThreshold: cl.MultiPartThreshold,
			UploadConcurrency:  cl.UploadConcurrency,
		},
		ObjectsRoot: gitrepo.LFSObjectsPath,
	}
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
	return resolveUploadSourcePathAt(gitrepo.LFSObjectsPath, oid, worktreePath, isPointer)
}

func resolveUploadSourcePathAt(objectsRoot string, oid string, worktreePath string, isPointer bool) (string, bool, error) {
	oid = localdrsobject.NormalizeOid(oid)
	if oid == "" {
		return "", false, fmt.Errorf("empty oid")
	}

	lfsObjPath, err := lfs.ObjectPath(objectsRoot, oid)
	if err == nil {
		if st, statErr := os.Stat(lfsObjPath); statErr == nil && !st.IsDir() && st.Size() > 0 {
			return lfsObjPath, true, nil
		}
	}

	if isPointer {
		// Historical inventory identifies the committed pointer blob, but the
		// current worktree may already be hydrated with the payload. Verify the
		// hydrated file instead of assuming every pointer path is pointer-form
		// in the worktree.
		if st, statErr := os.Stat(worktreePath); statErr == nil && !st.IsDir() {
			if matches, hashErr := lfs.FileMatchesSHA256(worktreePath, oid); hashErr == nil && matches {
				return worktreePath, true, nil
			}
		}
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

func uploadFileForObject(rt *pushRuntime, ctx context.Context, drsObject *drsapi.DrsObject, filePath string) error {
	hInfo := hash.ConvertDrsChecksumsToHashInfo(drsObject.Checksums)
	rt.Logger.DebugContext(ctx, fmt.Sprintf("uploading file %s", hInfo.SHA256))
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
	if rt.Backend == nil {
		return fmt.Errorf("upload backend is required")
	}
	if forceMultipart {
		if err := syupload.Upload(ctx, rt.Backend, filePath, objectKey, drsObject.Id, rt.Scope.Bucket, scopedUploadMetadata(rt), false, true); err != nil {
			return fmt.Errorf("upload error: %w", err)
		}
		return nil
	}
	if err := syupload.Upload(ctx, rt.Backend, filePath, objectKey, drsObject.Id, rt.Scope.Bucket, scopedUploadMetadata(rt), false, false); err != nil {
		return fmt.Errorf("upload error: %w", err)
	}
	return nil
}

func scopedUploadMetadata(rt *pushRuntime) sycommon.FileMetadata {
	organization := strings.TrimSpace(rt.Scope.Organization)
	project := strings.TrimSpace(rt.Scope.Project)
	if organization == "" || project == "" {
		return sycommon.FileMetadata{}
	}
	return sycommon.FileMetadata{
		Authorizations: syaccess.AuthzMapFromScope(organization, project),
	}
}
