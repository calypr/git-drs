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
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	internaltransfer "github.com/calypr/git-drs/internal/transfer"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/spf13/cobra"
)

var includePatterns []string
var dryRun bool

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

		inventory, err := loadWorktreeInventory(logg)
		if err != nil {
			return fmt.Errorf("failed to discover pointer files in worktree: %w", err)
		}
		pointers := collectPointerFiles(inventory, includePatterns)
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

		progress := internaltransfer.NewPullProgressRenderer(os.Stderr)
		progress.OnPlan(toPullFiles(pointers))
		defer func() {
			if finishErr := progress.Finish(); retErr == nil && finishErr != nil {
				retErr = fmt.Errorf("finalize pull progress: %w", finishErr)
			}
		}()

		ctx := context.Background()
		missingOIDs := make([]string, 0, len(pointers))
		seenMissing := make(map[string]struct{}, len(pointers))
		for _, f := range pointers {
			cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, f.Oid)
			if err != nil {
				return fmt.Errorf("failed to resolve LFS object path for %s: %w", f.Oid, err)
			}
			state, err := inspectCachedObject(cachePath, f.Oid, f.Size)
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

		if len(missingOIDs) > 0 {
			prefetched := make(map[string]drsapi.DrsObject, len(missingOIDs))
			for _, oid := range missingOIDs {
				recs, err := lookup.ObjectsByHashForScope(ctx, drsCtx, oid)
				if err != nil || len(recs) == 0 {
					continue
				}
				prefetched[oid] = recs[0]
			}
			if len(prefetched) > 0 {
				logg.Debug(fmt.Sprintf("prefetched %d objects for pull", len(prefetched)))
			} else {
				logg.Debug("bulk prefetch found no scoped objects; continuing per-object")
			}

			prefetchedAccess := make(map[string]drsapi.AccessURL, len(prefetched))
			if len(prefetched) > 0 {
				objects := make([]drsapi.DrsObject, 0, len(prefetched))
				for _, obj := range prefetched {
					objects = append(objects, obj)
				}
				if resolved, err := internaltransfer.BulkAccessURLsForObjects(ctx, drsCtx, objects); err == nil {
					prefetchedAccess = resolved
					logg.Debug(fmt.Sprintf("bulk access resolved %d URLs for pull", len(prefetchedAccess)))
				} else {
					logg.Debug(fmt.Sprintf("bulk access prefetch failed; continuing per-object: %v", err))
				}
			}
			for _, f := range pointers {
				dstPath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, f.Oid)
				if err != nil {
					return fmt.Errorf("failed to resolve LFS object path for %s: %w", f.Oid, err)
				}
				state, err := inspectCachedObject(dstPath, f.Oid, f.Size)
				if err == nil && state.complete {
					continue
				} else if err != nil {
					return fmt.Errorf("failed to stat cache path %s: %w", dstPath, err)
				}
				if state.exists {
					if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("failed to remove incomplete cached object %s: %w", dstPath, err)
					}
				}
				progress.OnDownloadStart(toPullFile(f))
				downloadCtx := progressContextForPointer(ctx, progress, f)
				if obj, ok := prefetched[f.Oid]; ok {
					if accessURL, ok := prefetchedAccess[obj.Id]; ok {
						objCopy := obj
						if err := internaltransfer.DownloadResolvedToCachePath(downloadCtx, drsCtx, f.Oid, dstPath, &objCopy, &accessURL); err != nil {
							debugCtx := buildPullDownloadDebugContext(ctx, drsCtx, f.Oid)
							return fmt.Errorf("failed to download oid %s to %s: %w\npull-debug: %s", f.Oid, dstPath, err, debugCtx)
						}
						if err := verifyObjectAtPath(dstPath, f.Oid, f.Size); err != nil {
							_ = os.Remove(dstPath)
							return fmt.Errorf("downloaded invalid cached object for oid %s: %w", f.Oid, err)
						}
						continue
					}
				}
				if err := internaltransfer.DownloadToCachePath(downloadCtx, drsCtx, f.Oid, dstPath); err != nil {
					debugCtx := buildPullDownloadDebugContext(ctx, drsCtx, f.Oid)
					return fmt.Errorf("failed to download oid %s to %s: %w\npull-debug: %s", f.Oid, dstPath, err, debugCtx)
				}
				if err := verifyObjectAtPath(dstPath, f.Oid, f.Size); err != nil {
					_ = os.Remove(dstPath)
					return fmt.Errorf("downloaded invalid cached object for oid %s: %w", f.Oid, err)
				}
			}
		} else {
			logg.Debug("no missing pointer objects to download")
		}

		if err := checkoutDownloadedFiles(pointers, progress); err != nil {
			return err
		}
		if err := refreshGitIndexForHydratedFiles(pointers); err != nil {
			return err
		}

		return nil
	},
}

type pointerFile struct {
	Name string
	Oid  string
	Size int64
}

func collectPointerFiles(inventory map[string]lfs.LfsFileInfo, patterns []string) []pointerFile {
	keys := make([]string, 0, len(inventory))
	for path := range inventory {
		if !matchesAnyPattern(path, patterns) {
			continue
		}
		keys = append(keys, path)
	}
	sort.Strings(keys)

	files := make([]pointerFile, 0, len(keys))
	for _, path := range keys {
		info := inventory[path]
		files = append(files, pointerFile{Name: path, Oid: info.Oid, Size: info.Size})
	}
	return files
}

func progressContextForPointer(ctx context.Context, progress *internaltransfer.PullProgressRenderer, file pointerFile) context.Context {
	ctx = sycommon.WithOid(ctx, file.Name)
	return sycommon.WithProgress(ctx, func(ev sycommon.ProgressEvent) error {
		if ev.Event != "progress" {
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

func matchesAnyPattern(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	normalized := filepath.ToSlash(filepath.Clean(path))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if matchesPattern(normalized, pattern) {
			return true
		}
	}
	return false
}

type cachedObjectState struct {
	exists   bool
	complete bool
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
	if info.IsDir() {
		return state, fmt.Errorf("cached object path is a directory: %s", path)
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return state, nil
	}
	if expectedSize <= 0 && info.Size() <= 0 {
		return state, nil
	}
	if strings.TrimSpace(expectedOID) == "" {
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
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func matchesPattern(path, pattern string) bool {
	pattern = filepath.ToSlash(filepath.Clean(pattern))
	if !strings.ContainsAny(pattern, "*?[") {
		return path == pattern
	}
	re, err := regexp.Compile(globToRegexp(pattern))
	if err != nil {
		return false
	}
	return re.MatchString(path)
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteString("$")
	return b.String()
}

func checkoutDownloadedFiles(files []pointerFile, progress *internaltransfer.PullProgressRenderer) error {
	for _, f := range files {
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Oid) == "" {
			continue
		}
		srcPath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, f.Oid)
		if err != nil {
			return fmt.Errorf("failed to resolve cached object for %s: %w", f.Oid, err)
		}
		if err := verifyObjectAtPath(srcPath, f.Oid, f.Size); err != nil {
			return fmt.Errorf("refusing to checkout invalid cached object for %s: %w", f.Oid, err)
		}
		src, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("failed to read cached object %s: %w", srcPath, err)
		}
		progress.OnCheckoutStart(toPullFile(f))
		if dir := filepath.Dir(f.Name); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				src.Close()
				return fmt.Errorf("failed to create directory for %s: %w", f.Name, err)
			}
		}
		dst, err := os.OpenFile(f.Name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			src.Close()
			return fmt.Errorf("failed to checkout %s: %w", f.Name, err)
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			src.Close()
			return fmt.Errorf("failed to checkout %s: %w", f.Name, err)
		}
		if err := dst.Close(); err != nil {
			src.Close()
			return fmt.Errorf("failed to finalize checkout for %s: %w", f.Name, err)
		}
		if err := src.Close(); err != nil {
			return fmt.Errorf("failed to close cached object %s: %w", srcPath, err)
		}
		if err := verifyObjectAtPath(f.Name, f.Oid, f.Size); err != nil {
			if removeErr := os.Remove(f.Name); removeErr != nil && !os.IsNotExist(removeErr) {
				return fmt.Errorf("checked out invalid content for %s: %w (cleanup failed: %v)", f.Name, err, removeErr)
			}
			return fmt.Errorf("checked out invalid content for %s: %w", f.Name, err)
		}
		progress.OnCompleted(toPullFile(f))
	}
	return nil
}

func refreshGitIndexForHydratedFiles(files []pointerFile) error {
	paths := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for _, f := range files {
		path := strings.TrimSpace(f.Name)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
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

func buildPullDownloadDebugContext(ctx context.Context, drsCtx *remoteruntime.GitContext, oid string) string {
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
			methods = append(methods, fmt.Sprintf("{type=%s access_id=%s url_scheme=%s url=%s}", am.Type, accessID, scheme, rawURL))
		}
	}
	return fmt.Sprintf("oid=%s did=%s size=%d access_methods=%s", oid, strings.TrimSpace(match.Id), match.Size, strings.Join(methods, ", "))
}

func init() {
	Cmd.Flags().StringArrayVarP(&includePatterns, "include", "I", nil, "include pathspec/glob pattern(s)")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list matching pointer files without downloading them")
}
