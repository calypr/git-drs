// Package filter implements the git long-running filter-process protocol v2
// (https://git-scm.com/docs/gitattributes#_long_running_filter_process) for
// git-drs. It is configured as the filter.lfs.process handler and intercepts
// smudge (checkout) and clean (stage) operations, wiring them directly to the
// DRS transfer stack without spawning a separate transfer agent.
//
// The command is hidden and invoked automatically by git when
//
//	filter.drs.process = git-drs filter
//
// is set in the repository config (written by `git drs init`).
package filter

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	internalfilter "github.com/calypr/git-drs/internal/filter"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/git-drs/internal/resolver"
	internaltransfer "github.com/calypr/git-drs/internal/transfer"
	"github.com/spf13/cobra"
)

// Cmd is the hidden cobra command registered in cmd/root.go.
var Cmd = &cobra.Command{
	Use:     "filter",
	Aliases: []string{"filter-process"},
	Short:   "Run git-drs as a git long-running filter process (invoked by git)",
	Hidden:  true,
	Args:    cobra.NoArgs,
	RunE:    runFilter,
}

func runFilter(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logger := drslog.GetLogger()
	logger.Debug("Starting filter")
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Debug(fmt.Sprintf("We should probably fix this: %v", err))
		return fmt.Errorf("filter: load config: %w", err)
	}

	var drsCtx *remoteruntime.GitContext
	var terraResolver resolver.Resolver

	remote, err := cfg.GetDefaultRemote()
	if err != nil {
		logger.Info("filter: no default remote", "err", err)
	} else {
		drsCtx, err = remoteruntime.New(cfg, remote, logger)
		if err != nil {
			logger.Info("DRS server not configured or unreachable", "err", err)
		} else if drsCtx.RemoteType == config.TerraServerType && !internalfilter.ShouldSkipSmudge() {
			if drsCtx.HubEndpoint != "" {
				terraResolver, err = resolver.NewTerraHub(ctx, drsCtx.HubEndpoint)
			} else {
				terraResolver, err = resolver.NewAnVIL(ctx, drsCtx.Endpoint)
			}
			if err != nil {
				return fmt.Errorf("filter: create Terra resolver: %w", err)
			}
		}
	}

	gitCommonDir, lfsRoot, err := lfs.GetGitRootDirectories(ctx)
	if err != nil {
		return fmt.Errorf("filter: resolve LFS root: %w", err)
	}
	logger.Debug("Resolved LFS root directory", "lfsRoot", lfsRoot)
	objectsRoot := filepath.Join(lfsRoot, "objects")
	drsObjectsRoot := filepath.Join(gitCommonDir, "drs", "lfs", "objects")
	if drsCtx != nil {
		worktreeRoot, err := gitrepo.GitTopLevel()
		if err != nil {
			return fmt.Errorf("filter: resolve worktree root: %w", err)
		}
		drsCtx.LFSObjectsRoot = objectsRoot
		drsCtx.RepositoryRoot = worktreeRoot
	}
	// Build the filter and register handlers.
	f := internalfilter.NewGitFilter(os.Stdin, os.Stdout, logger).
		OnSmudge(makeSmudgeHandlerWithRoot(drsCtx, terraResolver, objectsRoot, logger)).
		OnClean(makeCleanHandler(lfsRoot, drsObjectsRoot, logger))

	return f.Run(ctx)
}

// --------------------------------------------------------------------------
// Smudge handler — checkout: LFS pointer → real file content
// --------------------------------------------------------------------------

func makeSmudgeHandler(drsCtx *remoteruntime.GitContext, terraResolver resolver.Resolver, logger *slog.Logger) internalfilter.SmudgeFunc {
	objectsRoot, resolveErr := lfs.ResolveObjectsRoot(context.Background())
	handler := makeSmudgeHandlerWithRoot(drsCtx, terraResolver, objectsRoot, logger)
	return func(ctx context.Context, req internalfilter.FilterRequest, ptr io.Reader, dst io.Writer) error {
		if resolveErr != nil {
			return fmt.Errorf("filter: resolve LFS objects root: %w", resolveErr)
		}
		return handler(ctx, req, ptr, dst)
	}
}

func makeSmudgeHandlerWithRoot(drsCtx *remoteruntime.GitContext, terraResolver resolver.Resolver, objectsRoot string, logger *slog.Logger) internalfilter.SmudgeFunc {
	return func(ctx context.Context, req internalfilter.FilterRequest, ptr io.Reader, dst io.Writer) error {
		logger.Debug("smudge handler invoked", "pathname", req.Pathname)
		var downloadFn internalfilter.SmudgeDownloadFunc
		if drsCtx != nil && !internalfilter.ShouldSkipSmudge() {
			downloadFn = func(callCtx context.Context, oid, cachePath string) error {
				if terraResolver != nil {
					return resolver.DownloadToCache(callCtx, terraResolver, normalizeDRSOID(oid), cachePath)
				}
				return internaltransfer.DownloadToCachePath(callCtx, drsCtx, oid, cachePath)
			}
		}
		return internalfilter.SmudgeContentWithObjectsRoot(ctx, objectsRoot, req.Pathname, ptr, dst, logger, downloadFn)
	}
}

func normalizeDRSOID(oid string) string {
	if len(oid) >= 2 && oid[:2] == "//" {
		return "drs:" + oid
	}
	return oid
}

// --------------------------------------------------------------------------
// Clean handler — stage: real file content → LFS pointer
// --------------------------------------------------------------------------

func makeCleanHandler(lfsRoot, drsObjectsRoot string, logger *slog.Logger) internalfilter.CleanFunc {
	return func(ctx context.Context, req internalfilter.FilterRequest, content io.Reader, dst io.Writer) error {
		logger.Debug("clean", "pathname", req.Pathname)
		return internalfilter.CleanContentWithRoots(ctx, lfsRoot, drsObjectsRoot, req.Pathname, content, dst, logger)
	}
}

// --------------------------------------------------------------------------
// GitContext alias — expose the client package type without a circular import.
// --------------------------------------------------------------------------
