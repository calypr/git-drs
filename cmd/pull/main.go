package pull

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drslookup"
	"github.com/calypr/git-drs/internal/drsmap"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/spf13/cobra"
)

var runCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

var Cmd = &cobra.Command{
	Use:   "pull [remote-name]",
	Short: "Pull using the standard Git + Git LFS flow",
	Long:  "Pull using the standard Git + Git LFS flow (git pull, git lfs pull, git lfs checkout).",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs pull --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		var remote config.Remote
		if len(args) > 0 {
			remote = config.Remote(args[0])
		} else {
			remote, err = cfg.GetDefaultRemote()
			if err != nil {
				logg.Error(fmt.Sprintf("Error getting remote: %v", err))
				return err
			}
		}

		drsCtx, err := cfg.GetRemoteClient(remote, logg)
		if err != nil {
			logg.Error(fmt.Sprintf("error creating DRS client: %s", err))
			return err
		}
		_ = drsCtx // Remote validation only.

		if out, err := runCommand("git", "pull", string(remote)); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git pull failed for remote %q: %s", remote, msg)
		}

		var parsed struct {
			Files []lfs.LfsFileInfo `json:"files"`
		}
		out, err := runCommand("git", "lfs", "ls-files", "--json")
		if err != nil {
			msg := commandMessage(out, err)
			if !isMissingGitLFS(msg) {
				return fmt.Errorf("git lfs ls-files failed: %s", msg)
			}
			lfsFiles, inventoryErr := lfs.GetAllLfsFiles(string(remote), "", []string{"HEAD"}, logg)
			if inventoryErr != nil {
				return fmt.Errorf("git lfs ls-files failed: %s; fallback inventory failed: %w", msg, inventoryErr)
			}
			parsed.Files = make([]lfs.LfsFileInfo, 0, len(lfsFiles))
			for _, f := range lfsFiles {
				parsed.Files = append(parsed.Files, f)
			}
		} else if err := lfsjsonUnmarshal(out, &parsed); err != nil {
			return fmt.Errorf("failed to parse git lfs ls-files output: %w", err)
		}

		ctx := context.Background()
		missingOIDs := make([]string, 0, len(parsed.Files))
		seenMissing := make(map[string]struct{}, len(parsed.Files))
		for _, f := range parsed.Files {
			if f.Downloaded {
				continue
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
				recs, err := drslookup.ObjectsByHashForScope(ctx, drsCtx, oid)
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
		}

		for _, f := range parsed.Files {
			if f.Downloaded {
				continue
			}
			dstPath, err := drsmap.GetObjectPath(common.LFS_OBJS_PATH, f.Oid)
			if err != nil {
				return fmt.Errorf("failed to resolve LFS object path for %s: %w", f.Oid, err)
			}
			if err := lfs.DownloadToCachePath(ctx, drsCtx, logg, f.Oid, dstPath); err != nil {
				debugCtx := buildPullDownloadDebugContext(ctx, drsCtx, f.Oid)
				return fmt.Errorf("failed to download oid %s to %s: %w\npull-debug: %s", f.Oid, dstPath, err, debugCtx)
			}
		}

		if out, err := runCommand("git", "lfs", "checkout"); err != nil {
			msg := commandMessage(out, err)
			if !isMissingGitLFS(msg) {
				return fmt.Errorf("git lfs checkout failed: %s", msg)
			}
		}
		if err := checkoutDownloadedFiles(parsed.Files); err != nil {
			return err
		}

		return nil
	},
}

func commandMessage(out []byte, err error) string {
	msg := strings.TrimSpace(string(out))
	if msg == "" && err != nil {
		msg = err.Error()
	}
	return msg
}

func isMissingGitLFS(msg string) bool {
	return strings.Contains(msg, "git: 'lfs' is not a git command")
}

func checkoutDownloadedFiles(files []lfs.LfsFileInfo) error {
	for _, f := range files {
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Oid) == "" {
			continue
		}
		srcPath, err := drsmap.GetObjectPath(common.LFS_OBJS_PATH, f.Oid)
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

var lfsjsonUnmarshal = func(data []byte, v any) error {
	return sonic.ConfigFastest.Unmarshal(data, v)
}

func buildPullDownloadDebugContext(ctx context.Context, drsCtx *config.GitContext, oid string) string {
	recs, err := drslookup.ObjectsByHashForScope(ctx, drsCtx, oid)
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
