package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
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
	API            *remoteruntime.GitContext
	Backend        sytransfer.MultipartBackend
	Credential     *conf.Credential
	Logger         *slog.Logger
	Scope          pushScope
	Tuning         pushTuning
	ObjectsRoot    string
	DRSObjectsRoot string
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
	}
}

func pendingPushUploadPath(rt *pushRuntime, oid string) (string, error) {
	if rt == nil || strings.TrimSpace(rt.DRSObjectsRoot) == "" {
		return "", fmt.Errorf("DRS objects root is required for pending upload state")
	}
	oid = localdrsobject.NormalizeOid(oid)
	if oid == "" {
		return "", fmt.Errorf("empty oid for pending upload state")
	}
	endpoint := ""
	if rt.API != nil {
		endpoint = strings.TrimRight(strings.TrimSpace(rt.API.Endpoint), "/")
	}
	identity := strings.Join([]string{
		endpoint,
		strings.TrimSpace(rt.Scope.Organization),
		strings.TrimSpace(rt.Scope.Project),
		strings.TrimSpace(rt.Scope.Bucket),
		strings.TrimSpace(rt.Scope.StoragePref),
		oid,
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return filepath.Join(filepath.Dir(rt.DRSObjectsRoot), "push-pending", hex.EncodeToString(sum[:])), nil
}

func hasPendingPushUpload(rt *pushRuntime, oid string) (bool, error) {
	if rt == nil || strings.TrimSpace(rt.DRSObjectsRoot) == "" {
		return false, nil
	}
	path, err := pendingPushUploadPath(rt, oid)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func markPendingPushUpload(rt *pushRuntime, oid string) error {
	path, err := pendingPushUploadPath(rt, oid)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	marker, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if os.IsExist(err) {
		marker, err = os.OpenFile(path, os.O_RDWR, 0o600)
	}
	if err != nil {
		return err
	}
	if _, err := marker.WriteString("pending\n"); err != nil {
		_ = marker.Close()
		return err
	}
	if err := marker.Sync(); err != nil {
		_ = marker.Close()
		return err
	}
	if err := marker.Close(); err != nil {
		return err
	}
	return syncPushMarkerDirectory(dir)
}

func clearPendingPushUpload(rt *pushRuntime, oid string) error {
	if rt == nil || strings.TrimSpace(rt.DRSObjectsRoot) == "" {
		return nil
	}
	path, err := pendingPushUploadPath(rt, oid)
	if err != nil {
		return err
	}
	if err := os.Remove(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return syncPushMarkerDirectory(filepath.Dir(path))
}

func syncPushMarkerDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
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
	objectsRoot, err := lfs.ResolveObjectsRoot(context.Background())
	if err != nil {
		return "", false, fmt.Errorf("resolve LFS objects root: %w", err)
	}
	return resolveUploadSourcePathAt(objectsRoot, oid, worktreePath, isPointer)
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

func uploadFileForCandidate(rt *pushRuntime, ctx context.Context, candidate uploadCandidate) error {
	return uploadFileForObjectWithSHA256(rt, ctx, candidate.obj, candidate.src, uploadChecksumForCandidate(candidate.file, candidate.obj))
}

func uploadChecksumForCandidate(file lfs.LfsFileInfo, drsObject *drsapi.DrsObject) string {
	want := objectSHA256(drsObject)
	if !file.Placeholder {
		return want
	}
	placeholderOID := localdrsobject.NormalizeOid(file.Oid)
	if pointerSHA256 := hash.NormalizeChecksum(file.SHA256); pointerSHA256 != "" && pointerSHA256 != placeholderOID {
		return pointerSHA256
	}
	if want == placeholderOID {
		return ""
	}
	return want
}

func uploadFileForObjectWithSHA256(rt *pushRuntime, ctx context.Context, drsObject *drsapi.DrsObject, filePath, expectedSHA256 string) error {
	hInfo := hash.ConvertDrsChecksumsToHashInfo(drsObject.Checksums)
	rt.Logger.DebugContext(ctx, fmt.Sprintf("uploading file %s", hInfo.SHA256))
	if expectedSHA256 != "" {
		matches, err := lfs.FileMatchesSHA256(filePath, expectedSHA256)
		if err != nil {
			return fmt.Errorf("verify upload source %s against SHA-256 %s: %w", filePath, expectedSHA256, err)
		}
		if !matches {
			return fmt.Errorf("upload source %s does not match SHA-256 %s", filePath, expectedSHA256)
		}
	}
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
