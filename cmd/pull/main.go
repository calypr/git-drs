package pull

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
	internalfilter "github.com/calypr/git-drs/internal/filter"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/pathspec"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/git-drs/internal/resolver"
	internaltransfer "github.com/calypr/git-drs/internal/transfer"
	drsapi "github.com/calypr/syfon/apigen/drs"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/hash"
	"github.com/spf13/cobra"
)

var includePatterns []string
var dryRun bool
var accessMethod string

var (
	loadCfg         = config.LoadConfig
	resolveRemote   = func(cfg *config.Config, name string) (config.Remote, error) { return cfg.GetRemoteOrDefault(name) }
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error) {
		return remoteruntime.New(cfg, remote, logger)
	}
	loadWorktreeInventory = lfs.GetTrackedLfsFiles
)

var Cmd = &cobra.Command{
	Use:   "pull [remote-name]",
	Short: "Download DRS pointer file content into the current checkout",
	Long:  "Hydrate DRS/Git-LFS pointer files in the current checkout. By default this mirrors git lfs pull semantics for the worktree rather than running git pull.",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs pull --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		logg := drslog.GetLogger()
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		inventory, err := loadWorktreeInventory(logg)
		if err != nil {
			return fmt.Errorf("failed to discover pointer files in worktree: %w", err)
		}
		gitPaths, err := gitrepo.ResolveRepositoryPaths(ctx)
		if err != nil {
			return fmt.Errorf("failed to resolve Git repository paths: %w", err)
		}
		pointers := collectPointerFiles(inventory, includePatterns, gitPaths.DRSObjectsDir())
		if len(pointers) == 0 {
			logg.Debug("no matching pointer files to hydrate")
			return nil
		}

		if dryRun {
			for _, f := range pointers {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), f.Name); err != nil {
					return err
				}
			}
			return nil
		}

		cfg, err := loadCfg()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		var remote config.Remote
		if len(args) > 0 {
			remote = config.Remote(args[0])
		} else {
			remote, err = resolveRemote(cfg, "")
			if err != nil {
				logg.Error(fmt.Sprintf("Error getting remote: %v", err))
				return err
			}
		}

		drsCtx, err := newRemoteClient(cfg, remote, logg)
		if err != nil {
			logg.Error(fmt.Sprintf("error creating DRS client: %s", err))
			return err
		}
		drsCtx.CommandAccessMethod = accessMethod
		var anvil resolver.Resolver
		if !drsCtx.CanDownload() || !drsCtx.CanResolve() {
			return fmt.Errorf("remote %q does not support resolving and downloading DRS objects", remote)
		}
		if drsCtx.IsReadOnly() {
			anvil, err = resolver.NewAnVIL(ctx, drsCtx.Endpoint)
			if err != nil {
				return err
			}
			if method, ok := strictAccessMethod(accessMethod, drsCtx.AccessMethodPolicy); ok {
				return fmt.Errorf("access-method requirement %q cannot be enforced for read-only AnVIL remotes", method)
			}
		}

		progress := internaltransfer.NewPullProgressRenderer(os.Stderr)
		progress.OnPlan(toPullFiles(pointers))
		progress.StartHeartbeat()
		defer func() {
			if finishErr := progress.Finish(); retErr == nil && finishErr != nil {
				retErr = fmt.Errorf("finalize pull progress: %w", finishErr)
			}
		}()

		objectsRoot, err := lfs.ResolveObjectsRoot(ctx)
		if err != nil {
			return fmt.Errorf("failed to resolve LFS objects root: %w", err)
		}
		worktreeRoot, err := gitrepo.GitTopLevel()
		if err != nil {
			return fmt.Errorf("failed to resolve checkout worktree root: %w", err)
		}
		drsCtx.LFSObjectsRoot = objectsRoot
		drsCtx.RepositoryRoot = worktreeRoot
		missingOIDs := make([]string, 0, len(pointers))
		initialStates := make([]cachedObjectState, len(pointers))
		seenMissing := make(map[string]struct{}, len(pointers))
		for i, f := range pointers {
			cachePath, err := lfs.ObjectPath(objectsRoot, f.Oid)
			if err != nil {
				return fmt.Errorf("failed to resolve LFS object path for %s: %w", f.Oid, err)
			}
			if _, err := os.Stat(cachePath); err == nil {
				progress.OnStage("Verifying cached file")
			}
			state, err := inspectCachedPointer(cachePath, f)
			initialStates[i] = state
			if err == nil && state.complete {
				continue
			} else if err != nil {
				return fmt.Errorf("failed to stat cached object for %s: %w", f.Oid, err)
			}
			if _, seen := seenMissing[f.Oid]; seen {
				continue
			}
			seenMissing[f.Oid] = struct{}{}
			missingOIDs = append(missingOIDs, f.Oid)
		}

		prefetched := make(map[string]drsapi.DrsObject, len(missingOIDs))
		unverifiedDownloads := make(map[string]bool)
		if len(missingOIDs) > 0 {
			prefetchedAccess := make(map[string]internaltransfer.ResolvedAccess, len(prefetched))
			if len(missingOIDs) == 1 && anvil == nil {
				oid := missingOIDs[0]
				progress.OnStage("Resolving DRS record and download access")
				var access internaltransfer.ResolvedAccess
				var obj *drsapi.DrsObject
				if lfs.IsDRSURI(oid) {
					access, obj, err = internaltransfer.ResolvedAccessURLForDRSURI(ctx, drsCtx, normalizeDRSPointerOID(oid))
				} else {
					access, obj, err = internaltransfer.ResolvedAccessURLForHashScope(ctx, drsCtx, oid)
				}
				if err != nil {
					return fmt.Errorf("resolve DRS record and download access for %s: %w", oid, err)
				}
				prefetched[oid] = *obj
				prefetchedAccess[obj.Id] = access
			} else {
				progress.OnStage("Looking up DRS records")
				checksumOIDs := make([]string, 0, len(missingOIDs))
				for _, oid := range missingOIDs {
					if lfs.IsDRSURI(oid) {
						if anvil == nil {
							obj, err := drsCtx.Client.DRS().GetObject(ctx, normalizeDRSPointerOID(oid))
							if err != nil {
								return fmt.Errorf("look up DRS record %s: %w", oid, err)
							}
							prefetched[oid] = obj
						}
						continue
					}
					checksumOIDs = append(checksumOIDs, oid)
				}
				recsByOID, err := lookup.ObjectsByHashesForScope(ctx, drsCtx, checksumOIDs)
				if err != nil {
					return fmt.Errorf("look up DRS records by checksum: %w", err)
				}
				for _, oid := range checksumOIDs {
					recs := recsByOID[oid]
					if len(recs) == 0 {
						return fmt.Errorf("no matching DRS record found for oid %s in the configured scope", oid)
					}
					prefetched[oid] = recs[0]
				}
			}
			if len(prefetched) > 0 && len(missingOIDs) > 1 {
				progress.OnStage("Requesting download access")
				objects := make([]drsapi.DrsObject, 0, len(prefetched))
				for _, obj := range prefetched {
					objects = append(objects, obj)
				}
				prefetchedAccess, err = internaltransfer.BulkResolvedAccessURLsForObjects(ctx, drsCtx, objects)
				if err != nil {
					return fmt.Errorf("resolve download access: %w", err)
				}
			}
			var globusDownloads []internaltransfer.GlobusDownload
			globusOIDs := make(map[string]bool)
			for _, f := range pointers {
				obj, ok := prefetched[f.Oid]
				if !ok {
					continue
				}
				access, ok := prefetchedAccess[obj.Id]
				if !ok || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(access.AccessURL.Url)), "globus://") {
					continue
				}
				cachePath, err := lfs.ObjectPath(objectsRoot, f.Oid)
				if err != nil {
					return err
				}
				objCopy := obj
				globusDownloads = append(globusDownloads, internaltransfer.GlobusDownload{OID: f.Oid, CachePath: cachePath, Object: &objCopy, AccessURL: access.AccessURL.Url, Placeholder: f.Placeholder, ObjectsRoot: objectsRoot, RepositoryRoot: worktreeRoot})
				globusOIDs[f.Oid] = true
				progress.OnExternalTransferStart(toPullFile(f))
			}
			if err := internaltransfer.DownloadGlobusBatch(ctx, drsCtx, globusDownloads); err != nil {
				return fmt.Errorf("Globus batch download failed: %w", err)
			}
			downloadedOIDs := make(map[string]bool)
			for i, f := range pointers {
				if unverifiedDownloads[f.Oid] {
					continue
				}
				dstPath, err := lfs.ObjectPath(objectsRoot, f.Oid)
				if err != nil {
					return fmt.Errorf("failed to resolve LFS object path for %s: %w", f.Oid, err)
				}
				state := initialStates[i]
				changed, err := cachedObjectChanged(dstPath, state)
				if err != nil {
					return fmt.Errorf("failed to stat cache path %s: %w", dstPath, err)
				}
				if globusOIDs[f.Oid] || downloadedOIDs[f.Oid] || changed {
					state, err = inspectCachedPointer(dstPath, f)
				}
				if err == nil && state.complete {
					continue
				} else if err != nil {
					return fmt.Errorf("failed to stat cache path %s: %w", dstPath, err)
				}
				if state.exists && !internaltransfer.IncompleteDownloadCheckpoint(dstPath, f.Size) {
					if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("failed to remove incomplete cached object %s: %w", dstPath, err)
					}
				}
				progress.OnDownloadStart(toPullFile(f))
				downloadCtx := progressContextForPointer(ctx, progress, f)
				if obj, ok := prefetched[f.Oid]; ok {
					access, ok := prefetchedAccess[obj.Id]
					if !ok {
						return fmt.Errorf("download access was not resolved for DRS record %s", obj.Id)
					}
					objCopy := obj
					if err := internaltransfer.DownloadResolvedToCachePathWithAccess(downloadCtx, drsCtx, f.Oid, dstPath, &objCopy, access, objectsRoot, worktreeRoot); err != nil {
						debugCtx := buildPullDownloadDebugContext(ctx, drsCtx, f.Oid)
						return fmt.Errorf("failed to download oid %s to %s: %w\npull-debug: %s", f.Oid, dstPath, err, debugCtx)
					}
					if !f.Placeholder && !lfs.IsDRSURI(f.Oid) && expectedPointerSHA256(f) != "" {
						unverifiedDownloads[f.Oid] = true
						continue
					}
					if err := verifyPointerAtPathWithProgress(dstPath, f, func(n int64) { progress.OnVerificationProgress(f.Name, n) }); err != nil {
						_ = os.Remove(dstPath)
						return fmt.Errorf("downloaded invalid cached object for oid %s: %w", f.Oid, err)
					}
					rememberVerifiedPath(dstPath, f)
					downloadedOIDs[f.Oid] = true
					continue
				}
				if anvil == nil || !lfs.IsDRSURI(f.Oid) {
					return fmt.Errorf("DRS record was not resolved for oid %s", f.Oid)
				}
				if err := resolver.DownloadToCache(downloadCtx, anvil, normalizeDRSPointerOID(f.Oid), dstPath); err != nil {
					debugCtx := buildPullDownloadDebugContext(ctx, drsCtx, f.Oid)
					return fmt.Errorf("failed to download DRS URI %s to %s: %w\npull-debug: %s", f.Oid, dstPath, err, debugCtx)
				}
				if err := verifyPointerAtPathWithProgress(dstPath, f, func(n int64) { progress.OnVerificationProgress(f.Name, n) }); err != nil {
					_ = os.Remove(dstPath)
					return fmt.Errorf("downloaded invalid cached object for oid %s: %w", f.Oid, err)
				}
				rememberVerifiedPath(dstPath, f)
				downloadedOIDs[f.Oid] = true
			}
		} else {
			logg.Debug("no missing pointer objects to download")
		}
		if err := savePlaceholderChecksums(ctx, drsCtx, pointers, prefetched, objectsRoot, gitPaths.DRSObjectsDir()); err != nil {
			return err
		}
		if len(unverifiedDownloads) == 0 && alreadyHydratedInGit(ctx, pointers, worktreeRoot) {
			for _, f := range pointers {
				progress.OnCompleted(toPullFile(f))
			}
			return nil
		}

		readOnly := drsCtx.IsReadOnly()
		receipts, err := checkoutDownloadedFiles(ctx, pointers, progress, readOnly, objectsRoot, unverifiedDownloads)
		if err != nil {
			return err
		}
		progress.OnIndexRefreshStart()
		if err := refreshGitIndexForHydratedFiles(pointers, receipts); err != nil {
			return err
		}
		for _, f := range pointers {
			progress.OnCompleted(toPullFile(f))
		}

		return nil
	},
}

func normalizeDRSPointerOID(oid string) string {
	if strings.HasPrefix(oid, "//") {
		return "drs:" + oid
	}
	return oid
}

func strictAccessMethod(command, remotePolicy string) (string, bool) {
	if command = strings.TrimSpace(command); command != "" {
		return strings.ToLower(command), true
	}
	raw := strings.TrimSpace(os.Getenv("GIT_DRS_ACCESS_METHOD"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("GIT_DRS_TRANSFER_PROVIDER"))
	}
	if raw == "" {
		raw = remotePolicy
	}
	mode, method, found := strings.Cut(strings.ToLower(strings.TrimSpace(raw)), ":")
	if found && mode == "require" && method != "" {
		return method, true
	}
	return "", false
}

type pointerFile struct {
	Name        string
	Oid         string
	Size        int64
	SHA256      string
	Placeholder bool
}

func collectPointerFiles(inventory map[string]lfs.LfsFileInfo, patterns []string, drsObjectsRoot string) []pointerFile {
	keys := make([]string, 0, len(inventory))
	for path := range inventory {
		if !pathspec.MatchesAnyPattern(path, patterns) {
			continue
		}
		keys = append(keys, path)
	}
	sort.Strings(keys)

	files := make([]pointerFile, 0, len(keys))
	for _, path := range keys {
		info := inventory[path]
		sha256 := info.SHA256
		if info.Placeholder && sha256 == "" {
			if obj, err := localdrsobject.ReadObject(drsObjectsRoot, info.Oid); err == nil {
				sha256 = objectSHA256(obj)
			}
		}
		files = append(files, pointerFile{Name: path, Oid: info.Oid, Size: info.Size, SHA256: sha256, Placeholder: info.Placeholder})
	}
	return files
}

func inspectCachedPointer(path string, file pointerFile) (cachedObjectState, error) {
	if internaltransfer.IncompleteDownloadCheckpoint(path, file.Size) {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			return cachedObjectState{}, nil
		}
		if err != nil {
			return cachedObjectState{}, err
		}
		return cachedObjectState{exists: true, info: info}, nil
	}
	if info, ok := verifiedCacheInfo(path, file); ok {
		return cachedObjectState{exists: true, complete: true, info: info}, nil
	}
	expectedOID := file.Oid
	if file.Placeholder {
		expectedOID = ""
	}
	state, err := inspectCachedObject(path, expectedOID, file.Size)
	if err != nil || !state.complete {
		return state, err
	}
	if file.SHA256 != "" && (file.Placeholder || !strings.EqualFold(hash.NormalizeChecksum(expectedOID), file.SHA256)) {
		actual, err := calculateFileSHA256(path)
		if err != nil {
			return state, err
		}
		state.complete = strings.EqualFold(actual, file.SHA256)
	}
	rememberVerifiedCache(path, file, state)
	return state, nil
}

func verifyPointerAtPath(path string, file pointerFile) error {
	return verifyPointerAtPathWithProgress(path, file, nil)
}

func verifyPointerAtPathWithProgress(path string, file pointerFile, onProgress func(int64)) error {
	expectedOID := file.Oid
	if file.Placeholder {
		expectedOID = ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || (file.Size >= 0 && info.Size() != file.Size) {
		return fmt.Errorf("object at %s does not match expected oid/size", path)
	}
	checkOID := strings.TrimSpace(expectedOID) != "" && !lfs.IsDRSURI(expectedOID)
	if !checkOID && file.SHA256 == "" {
		return nil
	}
	if onProgress != nil {
		onProgress(0)
	}
	actual, err := calculateFileSHA256WithProgress(path, onProgress)
	if err != nil {
		return err
	}
	if checkOID && !strings.EqualFold(actual, strings.TrimPrefix(expectedOID, "sha256:")) {
		return fmt.Errorf("object at %s does not match expected oid/size", path)
	}
	if file.SHA256 != "" && !strings.EqualFold(actual, file.SHA256) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", file.SHA256, actual)
	}
	return nil
}

func progressContextForPointer(ctx context.Context, progress *internaltransfer.PullProgressRenderer, file pointerFile) context.Context {
	ctx = internaltransfer.WithResumableDownload(ctx)
	ctx = sycommon.WithOid(ctx, file.Name)
	return sycommon.WithProgress(ctx, func(ev sycommon.ProgressEvent) error {
		if ev.Event != "progress" {
			switch ev.Event {
			case "access-resolved":
				progress.OnConnectionStart(file.Name)
			case "transfer-start":
				progress.OnTransferStart(file.Name)
			case "transfer-restart":
				progress.OnDownloadRestart(file.Name)
			}
			return nil
		}
		progress.OnDownloadProgress(file.Name, ev.BytesSoFar, file.Size)
		return nil
	})
}

func toPullFiles(files []pointerFile) []internaltransfer.PullFile {
	out := make([]internaltransfer.PullFile, 0, len(files))
	for _, file := range files {
		out = append(out, toPullFile(file))
	}
	return out
}

func toPullFile(file pointerFile) internaltransfer.PullFile {
	return internaltransfer.PullFile{Name: file.Name, Oid: file.Oid, Size: file.Size}
}

type cachedObjectState struct {
	exists   bool
	complete bool
	info     os.FileInfo
}

func cachedObjectChanged(path string, state cachedObjectState) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return state.exists, nil
	}
	if err != nil {
		return false, err
	}
	if state.info == nil {
		return true, nil
	}
	return !os.SameFile(info, state.info) || info.Size() != state.info.Size() || !info.ModTime().Equal(state.info.ModTime()), nil
}

func inspectCachedObject(path, expectedOID string, expectedSize int64) (cachedObjectState, error) {
	var state cachedObjectState
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	state.exists = true
	state.info = info
	if info.IsDir() {
		return state, fmt.Errorf("cached object path is a directory: %s", path)
	}
	if expectedSize >= 0 && info.Size() != expectedSize {
		return state, nil
	}
	if strings.TrimSpace(expectedOID) == "" || strings.HasPrefix(strings.TrimSpace(expectedOID), "//") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(expectedOID)), "drs://") {
		state.complete = true
		return state, nil
	}

	actualOID, err := calculateFileSHA256(path)
	if err != nil {
		return state, err
	}
	state.complete = strings.EqualFold(strings.TrimPrefix(expectedOID, "sha256:"), actualOID)
	return state, nil
}

func verifyObjectAtPath(path, expectedOID string, expectedSize int64) error {
	state, err := inspectCachedObject(path, expectedOID, expectedSize)
	if err != nil {
		return err
	}
	if !state.exists {
		return fmt.Errorf("object missing at %s", path)
	}
	if !state.complete {
		return fmt.Errorf("object at %s does not match expected oid/size", path)
	}
	return nil
}

func calculateFileSHA256(path string) (string, error) {
	return calculateFileSHA256WithProgress(path, nil)
}

func calculateFileSHA256WithProgress(path string, onProgress func(int64)) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if onProgress == nil {
		if _, err := io.Copy(hasher, file); err != nil {
			return "", err
		}
	} else {
		buf := make([]byte, 4<<20)
		var read, reported int64
		for {
			n, readErr := file.Read(buf)
			if n > 0 {
				if _, err := hasher.Write(buf[:n]); err != nil {
					return "", err
				}
				read += int64(n)
				if read-reported >= 64<<20 {
					onProgress(read)
					reported = read
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return "", readErr
			}
		}
		onProgress(read)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func objectSHA256(obj *drsapi.DrsObject) string {
	if obj == nil {
		return ""
	}
	for _, checksum := range obj.Checksums {
		checksumType := strings.ToLower(strings.TrimSpace(checksum.Type))
		if checksumType != "sha256" && checksumType != "sha-256" {
			continue
		}
		sha256 := strings.ToLower(hash.NormalizeChecksum(checksum.Checksum))
		if len(sha256) == 64 && strings.Trim(sha256, "0123456789abcdef") == "" {
			return sha256
		}
	}
	return ""
}

func savePlaceholderChecksums(ctx context.Context, drsCtx *remoteruntime.GitContext, files []pointerFile, objects map[string]drsapi.DrsObject, objectsRoot, drsObjectsRoot string) error {
	saved := make(map[string]string)
	for i := range files {
		file := &files[i]
		if !file.Placeholder {
			continue
		}
		if actual, ok := saved[file.Oid]; ok {
			file.SHA256 = actual
			continue
		}
		cachePath, err := lfs.ObjectPath(objectsRoot, file.Oid)
		if err != nil {
			return err
		}
		actual := file.SHA256
		if actual == "" {
			actual, err = calculateFileSHA256(cachePath)
			if err != nil {
				return fmt.Errorf("calculate sha256 for placeholder oid %s: %w", file.Oid, err)
			}
		}
		file.SHA256 = actual
		saved[file.Oid] = actual

		var obj drsapi.DrsObject
		local, localErr := localdrsobject.ReadObject(drsObjectsRoot, file.Oid)
		if localErr == nil {
			obj = *local
		}
		prefetched, remoteFound := objects[file.Oid]
		if !remoteFound && objectSHA256(&obj) == "" && drsCtx != nil && drsCtx.Client != nil {
			records, err := lookup.ObjectsByHashForScope(ctx, drsCtx, file.Oid)
			if err != nil {
				return fmt.Errorf("resolve placeholder oid %s before saving sha256: %w", file.Oid, err)
			}
			if len(records) == 0 {
				return fmt.Errorf("resolve placeholder oid %s before saving sha256: no matching Syfon record", file.Oid)
			}
			prefetched, remoteFound = records[0], true
		}
		if localErr != nil {
			if !remoteFound {
				return fmt.Errorf("resolve placeholder oid %s before saving sha256: metadata unavailable", file.Oid)
			}
			obj = prefetched
		}
		if expected := objectSHA256(&obj); expected != "" && !strings.EqualFold(expected, actual) {
			return fmt.Errorf("downloaded placeholder oid %s has sha256 %s, expected %s", file.Oid, actual, expected)
		}
		if remoteFound {
			if expected := objectSHA256(&prefetched); expected != "" && !strings.EqualFold(expected, actual) {
				return fmt.Errorf("downloaded placeholder oid %s has sha256 %s, expected %s", file.Oid, actual, expected)
			}
		}
		checksums := make([]drsapi.Checksum, 0, len(obj.Checksums)+1)
		for _, checksum := range obj.Checksums {
			checksumType := strings.ToLower(strings.TrimSpace(checksum.Type))
			if checksumType != "sha256" && checksumType != "sha-256" {
				checksums = append(checksums, checksum)
			}
		}
		obj.Checksums = append(checksums, drsapi.Checksum{Type: "sha256", Checksum: actual})
		if err := localdrsobject.WriteObject(drsObjectsRoot, &obj, file.Oid); err != nil {
			return fmt.Errorf("save sha256 for placeholder oid %s: %w", file.Oid, err)
		}
		rememberVerifiedPath(cachePath, *file)
	}
	return nil
}

func checkoutDownloadedFiles(ctx context.Context, files []pointerFile, progress *internaltransfer.PullProgressRenderer, readOnly bool, objectsRoot string, unverifiedDownloads map[string]bool) ([]internalfilter.IndexRefreshReceipt, error) {
	worktreeRoot, err := gitrepo.GitTopLevel()
	if err != nil {
		// Unit-level callers may provide an isolated checkout directory without
		// initializing Git. The command path always has an inventory from Git,
		// so this fallback preserves that helper's historical behavior.
		worktreeRoot, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve checkout worktree root: %w", err)
		}
	}
	receipts := make([]internalfilter.IndexRefreshReceipt, 0, len(files))
	for _, f := range files {
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Oid) == "" {
			continue
		}
		srcPath, err := lfs.ObjectPath(objectsRoot, f.Oid)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve cached object for %s: %w", f.Oid, err)
		}
		freshDownload := unverifiedDownloads[f.Oid]
		var state cachedObjectState
		if freshDownload {
			info, err := os.Stat(srcPath)
			if err != nil {
				return nil, fmt.Errorf("stat downloaded cache object for %s: %w", f.Oid, err)
			}
			if !info.Mode().IsRegular() || info.Size() != f.Size || internaltransfer.IncompleteDownloadCheckpoint(srcPath, f.Size) {
				return nil, fmt.Errorf("downloaded cache object for %s is incomplete", f.Oid)
			}
			state = cachedObjectState{exists: true, info: info}
		} else {
			state, err = inspectCachedPointer(srcPath, f)
			if err != nil {
				return nil, fmt.Errorf("refusing to checkout invalid cached object for %s: %w", f.Oid, err)
			}
			if !state.complete {
				return nil, fmt.Errorf("refusing to checkout invalid cached object for %s", f.Oid)
			}
		}
		src, err := os.Open(srcPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read cached object %s: %w", srcPath, err)
		}
		dstPath, err := gitrepo.SafeWorktreePath(worktreeRoot, f.Name)
		if err != nil {
			src.Close()
			return nil, fmt.Errorf("refusing to checkout %s: %w", f.Name, err)
		}
		if !freshDownload && expectedPointerSHA256(f) != "" {
			if info, statErr := os.Lstat(dstPath); statErr == nil && info.Mode().IsRegular() && info.Size() == f.Size {
				progress.OnExistingFileVerificationStart(toPullFile(f))
				before := info
				if verifyPointerAtPathWithProgress(dstPath, f, func(n int64) { progress.OnExistingFileProgress(f.Name, n) }) == nil {
					if after, statErr := os.Lstat(dstPath); statErr == nil {
						if receipt, ok := internalfilter.NewIndexRefreshReceipt(f.Name, f.Oid, expectedPointerSHA256(f), f.Size, after); ok && receipt.Matches(f.Name, before) {
							receipts = append(receipts, receipt)
						}
					}
					if err := src.Close(); err != nil {
						return nil, fmt.Errorf("close cached object for %s: %w", f.Name, err)
					}
					continue
				}
			}
		}
		progress.OnCheckoutStart(toPullFile(f))
		if dir := filepath.Dir(dstPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				src.Close()
				return nil, fmt.Errorf("failed to create directory for %s: %w", f.Name, err)
			}
		}
		mode := os.FileMode(0o644)
		if info, statErr := os.Lstat(dstPath); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				src.Close()
				return nil, fmt.Errorf("refusing to checkout through symlink %s", f.Name)
			}
			mode = info.Mode().Perm() | 0o200
		} else if !os.IsNotExist(statErr) {
			src.Close()
			return nil, fmt.Errorf("failed to inspect checkout path %s: %w", f.Name, statErr)
		}
		if dstPath, err = gitrepo.SafeWorktreePath(worktreeRoot, f.Name); err != nil {
			src.Close()
			return nil, fmt.Errorf("refusing to checkout %s: %w", f.Name, err)
		}
		if readOnly {
			mode = 0o444
		}
		receipt, hasReceipt, err := replaceCheckoutFile(ctx, dstPath, src, f, mode, func(n int64) { progress.OnCheckoutProgress(f.Name, n) })
		if err != nil {
			return nil, fmt.Errorf("failed to checkout %s: %w", f.Name, err)
		}
		if freshDownload {
			state.complete = true
			rememberVerifiedCache(srcPath, f, state)
			delete(unverifiedDownloads, f.Oid)
		}
		if hasReceipt {
			receipts = append(receipts, receipt)
		}
	}
	return receipts, nil
}

type checkoutProgressWriter struct {
	ctx        context.Context
	dst        io.Writer
	onProgress func(int64)
	bytes      int64
	reported   int64
}

func (w *checkoutProgressWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.dst.Write(p)
	w.bytes += int64(n)
	if w.onProgress != nil && w.bytes-w.reported >= 64<<20 {
		w.onProgress(w.bytes)
		w.reported = w.bytes
	}
	return n, err
}

func replaceCheckoutFile(ctx context.Context, dstPath string, src io.ReadCloser, pointer pointerFile, mode os.FileMode, onProgress func(int64)) (internalfilter.IndexRefreshReceipt, bool, error) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	defer func() {
		if src != nil {
			_ = src.Close()
		}
	}()
	stage, err := os.CreateTemp(filepath.Dir(dstPath), ".git-drs-checkout-*")
	if err != nil {
		return internalfilter.IndexRefreshReceipt{}, false, err
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	hasher := sha256.New()
	writer := &checkoutProgressWriter{ctx: ctx, dst: io.MultiWriter(stage, hasher), onProgress: onProgress}
	copied, err := io.Copy(writer, src)
	if err != nil {
		stage.Close()
		return internalfilter.IndexRefreshReceipt{}, false, err
	}
	if onProgress != nil {
		onProgress(copied)
	}
	if err := stage.Close(); err != nil {
		return internalfilter.IndexRefreshReceipt{}, false, err
	}
	if err := src.Close(); err != nil {
		return internalfilter.IndexRefreshReceipt{}, false, err
	}
	src = nil
	if pointer.Size >= 0 && copied != pointer.Size {
		return internalfilter.IndexRefreshReceipt{}, false, fmt.Errorf("checkout size mismatch: expected %d, got %d", pointer.Size, copied)
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if !pointer.Placeholder && strings.TrimSpace(pointer.Oid) != "" && !lfs.IsDRSURI(pointer.Oid) && !strings.EqualFold(hash.NormalizeChecksum(pointer.Oid), actual) {
		return internalfilter.IndexRefreshReceipt{}, false, fmt.Errorf("checkout sha256 mismatch: expected %s, got %s", pointer.Oid, actual)
	}
	if pointer.SHA256 != "" && !strings.EqualFold(pointer.SHA256, actual) {
		return internalfilter.IndexRefreshReceipt{}, false, fmt.Errorf("checkout sha256 mismatch: expected %s, got %s", pointer.SHA256, actual)
	}
	if err := os.Chmod(stagePath, mode); err != nil {
		return internalfilter.IndexRefreshReceipt{}, false, err
	}
	stagedInfo, err := os.Lstat(stagePath)
	if err != nil {
		return internalfilter.IndexRefreshReceipt{}, false, err
	}
	if err := os.Rename(stagePath, dstPath); err != nil {
		return internalfilter.IndexRefreshReceipt{}, false, err
	}
	checkedOutInfo, err := os.Lstat(dstPath)
	if err != nil {
		return internalfilter.IndexRefreshReceipt{}, false, nil
	}
	if !internalfilter.SameIndexRefreshFile(stagedInfo, checkedOutInfo) {
		return internalfilter.IndexRefreshReceipt{}, false, nil
	}
	receipt, ok := internalfilter.NewIndexRefreshReceipt(pointer.Name, pointer.Oid, actual, copied, checkedOutInfo)
	if !ok {
		return internalfilter.IndexRefreshReceipt{}, false, nil
	}
	return receipt, true, nil
}

func alreadyHydratedInGit(ctx context.Context, files []pointerFile, worktreeRoot string) bool {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		path, err := gitrepo.SafeWorktreePath(worktreeRoot, file.Name)
		if err != nil {
			return false
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != file.Size {
			return false
		}
		if info.Size() <= 2048 {
			content, err := os.ReadFile(path)
			if err != nil {
				return false
			}
			if _, _, pointer := lfs.ParseLFSPointer(content); pointer {
				return false
			}
		}
		paths = append(paths, file.Name)
	}
	if len(paths) == 0 {
		return false
	}
	flagArgs := append([]string{"ls-files", "-v", "-z", "--"}, paths...)
	flags, err := exec.CommandContext(ctx, "git", flagArgs...).Output()
	if err != nil {
		return false
	}
	listed := make(map[string]struct{}, len(paths))
	for _, entry := range strings.Split(strings.TrimSuffix(string(flags), "\x00"), "\x00") {
		if len(entry) < 3 || entry[:2] != "H " {
			return false
		}
		listed[entry[2:]] = struct{}{}
	}
	if len(listed) != len(paths) {
		return false
	}
	for _, path := range paths {
		if _, ok := listed[path]; !ok {
			return false
		}
	}
	manifestPath, err := createIndexRefreshManifest(paths, nil)
	if err != nil {
		return false
	}
	defer os.Remove(manifestPath)
	args := append([]string{"diff-files", "--quiet", "--"}, paths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = commandEnvWithOverrides(os.Environ(), map[string]string{
		internalfilter.IndexRefreshEnv:         "1",
		internalfilter.IndexRefreshReceiptsEnv: manifestPath,
	})
	return cmd.Run() == nil
}

func refreshGitIndexForHydratedFiles(files []pointerFile, receipts []internalfilter.IndexRefreshReceipt) error {
	paths := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	pathSet := make(map[string]struct{}, len(files))
	for _, f := range files {
		path := f.Name
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
		pathSet[filepath.ToSlash(filepath.Clean(path))] = struct{}{}
	}
	if len(paths) == 0 {
		return nil
	}

	// Re-run Git's clean filter on the just-hydrated paths so the index/worktree
	// bookkeeping matches stock LFS behavior. The hydrated bytes were already
	// verified against the pointer OID/size above, so this should be a
	// semantic no-op that only clears the false-dirty state.
	args := append([]string{"add", "--"}, paths...)
	cmd := exec.Command("git", args...)
	trustedReceipts := make([]internalfilter.IndexRefreshReceipt, 0, len(receipts))
	for _, receipt := range receipts {
		path := filepath.ToSlash(filepath.Clean(receipt.Path))
		if _, ok := pathSet[path]; ok {
			receipt.Path = path
			trustedReceipts = append(trustedReceipts, receipt)
		}
	}
	manifestPath, err := createIndexRefreshManifest(paths, trustedReceipts)
	if err != nil {
		return err
	}
	defer os.Remove(manifestPath)
	cmd.Env = commandEnvWithOverrides(os.Environ(), map[string]string{
		internalfilter.IndexRefreshEnv:         "1",
		internalfilter.IndexRefreshReceiptsEnv: manifestPath,
	})
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("failed to refresh git index for hydrated files: %w", err)
		}
		return fmt.Errorf("failed to refresh git index for hydrated files: %w: %s", err, msg)
	}
	return nil
}

func createIndexRefreshManifest(paths []string, receipts []internalfilter.IndexRefreshReceipt) (string, error) {
	data, err := internalfilter.MarshalIndexRefreshManifest(paths, receipts)
	if err != nil {
		return "", fmt.Errorf("encode index refresh manifest: %w", err)
	}
	manifest, err := os.CreateTemp("", "git-drs-index-refresh-*.json")
	if err != nil {
		return "", fmt.Errorf("create index refresh manifest: %w", err)
	}
	path := manifest.Name()
	if _, err := manifest.Write(data); err != nil {
		_ = manifest.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write index refresh manifest: %w", err)
	}
	if err := manifest.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close index refresh manifest: %w", err)
	}
	return path, nil
}

func commandEnvWithOverrides(env []string, overrides map[string]string) []string {
	result := make([]string, 0, len(env)+len(overrides))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := overrides[key]; replaced {
				continue
			}
		}
		result = append(result, entry)
	}
	for key, value := range overrides {
		if value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

func buildPullDownloadDebugContext(ctx context.Context, drsCtx *remoteruntime.GitContext, oid string) string {
	if drsCtx == nil {
		return fmt.Sprintf("oid=%s resolver=unavailable", oid)
	}
	if drsCtx.Client == nil {
		return fmt.Sprintf("oid=%s resolver=%s", oid, drsCtx.RemoteType)
	}
	recs, err := lookup.ObjectsByHashForScope(ctx, drsCtx, oid)
	if err != nil {
		return fmt.Sprintf("oid=%s query_error=%v", oid, err)
	}
	if len(recs) == 0 {
		return fmt.Sprintf("oid=%s records=0", oid)
	}

	match := &recs[0]

	methods := make([]string, 0)
	if match.AccessMethods != nil {
		methods = make([]string, 0, len(*match.AccessMethods))
		for _, am := range *match.AccessMethods {
			scheme := ""
			rawURL := ""
			if am.AccessUrl != nil {
				rawURL = strings.TrimSpace(am.AccessUrl.Url)
			}
			if rawURL != "" {
				if parsed, parseErr := url.Parse(rawURL); parseErr == nil {
					scheme = parsed.Scheme
				}
			}
			accessID := ""
			if am.AccessId != nil {
				accessID = strings.TrimSpace(*am.AccessId)
			}
			methods = append(methods, fmt.Sprintf("{type=%s access_id=%s url_scheme=%s}", am.Type, accessID, scheme))
		}
	}
	return fmt.Sprintf("oid=%s did=%s size=%d access_methods=%s", oid, strings.TrimSpace(match.Id), match.Size, strings.Join(methods, ", "))
}

func init() {
	Cmd.Flags().StringArrayVarP(&includePatterns, "include", "I", nil, "include pathspec/glob pattern(s)")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list matching pointer files without downloading them")
	Cmd.Flags().StringVar(&accessMethod, "access-method", "", "require one access method type (for example: globus or https)")
}
