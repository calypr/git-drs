package pull

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/calypr/git-drs/client"
	clientdrs "github.com/calypr/git-drs/client/drs"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	sycommon "github.com/calypr/syfon/client/common"
	datadrs "github.com/calypr/syfon/client/drs"
	"github.com/calypr/syfon/client/hash"
	sylogs "github.com/calypr/syfon/client/logs"
	"github.com/calypr/syfon/client/transfer"
	"github.com/calypr/syfon/client/xfer/download"
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

		out, err := runCommand("git", "lfs", "ls-files", "--json")
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git lfs ls-files failed: %s", msg)
		}
		var parsed struct {
			Files []lfs.LfsFileInfo `json:"files"`
		}
		if err := lfsjsonUnmarshal(out, &parsed); err != nil {
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
			if byHash, err := drsCtx.API.BatchGetObjectsByHash(ctx, missingOIDs); err == nil {
				prefetched := make(map[string]datadrs.DRSObject, len(missingOIDs))
				for _, oid := range missingOIDs {
					recs := byHash[oid]
					if len(recs) == 0 {
						continue
					}
					match, matchErr := drsmap.FindMatchingRecord(recs, drsCtx.Organization, drsCtx.ProjectId)
					if matchErr != nil || match == nil {
						continue
					}
					prefetched[oid] = *match
				}
				ctx = datadrs.WithPrefetchedBySHA(ctx, prefetched)
				logg.Debug(fmt.Sprintf("prefetched %d objects for pull", len(prefetched)))
			} else {
				logg.Debug(fmt.Sprintf("bulk prefetch failed; continuing per-object: %v", err))
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
			// Use the high-level download orchestrator to perform robust signed-URL downloads.
			downloader, ok := drsCtx.API.(transfer.Downloader)
			if !ok {
				return fmt.Errorf("drs client does not implement transfer.Downloader")
			}
			scopedDownloader := &gitScopedDownloader{
				base:    downloader,
				api:     drsCtx.API,
				org:     drsCtx.Organization,
				project: drsCtx.ProjectId,
				logger:  logg,
			}
			if err := download.DownloadFile(ctx, drsCtx.API, scopedDownloader, f.Oid, dstPath); err != nil {
				debugCtx := buildPullDownloadDebugContext(ctx, drsCtx, f.Oid)
				return fmt.Errorf("failed to download oid %s to %s: %w\npull-debug: %s", f.Oid, dstPath, err, debugCtx)
			}
		}

		if out, err := runCommand("git", "lfs", "checkout"); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git lfs checkout failed: %s", msg)
		}

		return nil
	},
}

var lfsjsonUnmarshal = func(data []byte, v any) error {
	return sonic.ConfigFastest.Unmarshal(data, v)
}

type gitScopedDownloader struct {
	base    transfer.Downloader
	api     datadrs.Client
	org     string
	project string
	logger  *slog.Logger
}

func (d *gitScopedDownloader) Name() string { return d.base.Name() }

func (d *gitScopedDownloader) Logger() *sylogs.Gen3Logger { return d.base.Logger() }

func (d *gitScopedDownloader) ResolveDownloadURL(ctx context.Context, guid string, accessID string) (string, error) {
	if strings.TrimSpace(accessID) != "" {
		return d.base.ResolveDownloadURL(ctx, guid, accessID)
	}
	accessURL, err := clientdrs.ResolveGitScopedURL(ctx, d.api, guid, d.org, d.project, d.logger)
	if err != nil {
		return "", err
	}
	if accessURL == nil || strings.TrimSpace(accessURL.Url) == "" {
		return "", fmt.Errorf("empty download URL for oid %s", guid)
	}
	return accessURL.Url, nil
}

func (d *gitScopedDownloader) Download(ctx context.Context, fdr *sycommon.FileDownloadResponseObject) (*http.Response, error) {
	return d.base.Download(ctx, fdr)
}

func buildPullDownloadDebugContext(ctx context.Context, drsCtx *client.GitContext, oid string) string {
	recs, err := drsCtx.API.GetObjectByHash(ctx, &hash.Checksum{Type: "sha256", Checksum: oid})
	if err != nil {
		return fmt.Sprintf("oid=%s query_error=%v", oid, err)
	}
	if len(recs) == 0 {
		return fmt.Sprintf("oid=%s records=0", oid)
	}

	match, matchErr := drsmap.FindMatchingRecord(recs, drsCtx.Organization, drsCtx.ProjectId)
	if matchErr != nil {
		return fmt.Sprintf("oid=%s records=%d match_error=%v", oid, len(recs), matchErr)
	}
	if match == nil {
		return fmt.Sprintf("oid=%s records=%d no_project_match org=%s project=%s", oid, len(recs), drsCtx.Organization, drsCtx.ProjectId)
	}

	methods := make([]string, 0, len(match.AccessMethods))
	for _, am := range match.AccessMethods {
		scheme := ""
		rawURL := strings.TrimSpace(am.AccessUrl.Url)
		if rawURL != "" {
			if parsed, parseErr := url.Parse(rawURL); parseErr == nil {
				scheme = parsed.Scheme
			}
		}
		accessID := strings.TrimSpace(am.AccessId)
		methods = append(methods, fmt.Sprintf("{type=%s access_id=%s url_scheme=%s url=%s}", am.Type, accessID, scheme, rawURL))
	}
	return fmt.Sprintf("oid=%s did=%s size=%d access_methods=%s", oid, strings.TrimSpace(match.Id), match.Size, strings.Join(methods, ", "))
}
