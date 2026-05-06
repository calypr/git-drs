package pull

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsremote"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/pathspec"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/spf13/cobra"
)

var includePatterns []string
var dryRun bool

var (
	loadCfg = config.LoadConfig
	resolveRemote = func(cfg *config.Config, name string) (config.Remote, error) { return cfg.GetRemoteOrDefault(name) }
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*config.GitContext, error) {
		return cfg.GetRemoteClient(remote, logger)
	}
	loadWorktreeInventory = lfs.GetWorktreeLfsFiles
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
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

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

		ctx := context.Background()
		missingOIDs := make([]string, 0, len(pointers))
		seenMissing := make(map[string]struct{}, len(pointers))
		for _, f := range pointers {
			cachePath, err := lfs.ObjectPath(common.LFS_OBJS_PATH, f.Oid)
			if err != nil {
				return fmt.Errorf("failed to resolve LFS object path for %s: %w", f.Oid, err)
			}
			if _, err := os.Stat(cachePath); err == nil {
				continue
			} else if !os.IsNotExist(err) {
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
				recs, err := drsremote.ObjectsByHashForScope(ctx, drsCtx, oid)
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
				if resolved, err := drsremote.BulkAccessURLsForObjects(ctx, drsCtx, objects); err == nil {
					prefetchedAccess = resolved
					logg.Debug(fmt.Sprintf("bulk access resolved %d URLs for pull", len(prefetchedAccess)))
				} else {
					logg.Debug(fmt.Sprintf("bulk access prefetch failed; continuing per-object: %v", err))
				}
			}
			for _, f := range pointers {
				dstPath, err := lfs.ObjectPath(common.LFS_OBJS_PATH, f.Oid)
				if err != nil {
					return fmt.Errorf("failed to resolve LFS object path for %s: %w", f.Oid, err)
				}
				if _, err := os.Stat(dstPath); err == nil {
					continue
				} else if !os.IsNotExist(err) {
					return fmt.Errorf("failed to stat cache path %s: %w", dstPath, err)
				}
				if obj, ok := prefetched[f.Oid]; ok {
					if accessURL, ok := prefetchedAccess[obj.Id]; ok {
						objCopy := obj
						if err := drsremote.DownloadResolvedToCachePath(ctx, drsCtx, f.Oid, dstPath, &objCopy, &accessURL); err != nil {
							debugCtx := buildPullDownloadDebugContext(ctx, drsCtx, f.Oid)
							return fmt.Errorf("failed to download oid %s to %s: %w\npull-debug: %s", f.Oid, dstPath, err, debugCtx)
						}
						continue
					}
				}
				if err := drsremote.DownloadToCachePath(ctx, drsCtx, logg, f.Oid, dstPath); err != nil {
					debugCtx := buildPullDownloadDebugContext(ctx, drsCtx, f.Oid)
					return fmt.Errorf("failed to download oid %s to %s: %w\npull-debug: %s", f.Oid, dstPath, err, debugCtx)
				}
			}
		} else {
			logg.Debug("no missing pointer objects to download")
		}

		if err := checkoutDownloadedFiles(pointers); err != nil {
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
		if !pathspec.MatchesAny(path, patterns) {
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

func checkoutDownloadedFiles(files []pointerFile) error {
	for _, f := range files {
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Oid) == "" {
			continue
		}
		srcPath, err := lfs.ObjectPath(common.LFS_OBJS_PATH, f.Oid)
		if err != nil {
			return fmt.Errorf("failed to resolve cached object for %s: %w", f.Oid, err)
		}
		payload, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("failed to read cached object %s: %w", srcPath, err)
		}
		if err := os.WriteFile(f.Name, payload, 0o644); err != nil {
			return fmt.Errorf("failed to checkout %s: %w", f.Name, err)
		}
	}
	return nil
}

func buildPullDownloadDebugContext(ctx context.Context, drsCtx *config.GitContext, oid string) string {
	recs, err := drsremote.ObjectsByHashForScope(ctx, drsCtx, oid)
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
