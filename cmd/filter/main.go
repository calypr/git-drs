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

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/client/downloader"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

// Cmd is the hidden cobra command registered in cmd/root.go.
var Cmd = &cobra.Command{
	Use:    "filter",
	Aliases: []string{"filter-process"},
	Short:  "Run git-drs as a git long-running filter process (invoked by git)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runFilter,
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

	var drsCtx *client.GitContext

	remote, err := cfg.GetDefaultRemote()
	if err != nil {
		logger.Info("filter: no default remote", "err", err)
	} else {
		drsCtx, err = cfg.GetRemoteClient(remote, logger)
		if err != nil {
			logger.Info("DRS server not configured or unreachable", "err", err)
		}
	}

	_, lfsRoot, err := lfs.GetGitRootDirectories(ctx)
	if err != nil {
		return fmt.Errorf("filter: resolve LFS root: %w", err)
	}
	logger.Debug("Resolved LFS root directory", "lfsRoot", lfsRoot)
	// Build the filter and register handlers.
	f := lfs.NewGitFilter(os.Stdin, os.Stdout, logger).
		OnSmudge(makeSmudgeHandler(drsCtx, logger)).
		OnClean(makeCleanHandler(lfsRoot, logger))

	return f.Run(ctx)
}

// --------------------------------------------------------------------------
// Smudge handler — checkout: LFS pointer → real file content
// --------------------------------------------------------------------------

func makeSmudgeHandler(drsCtx *client.GitContext, logger *slog.Logger) lfs.SmudgeFunc {
	return func(ctx context.Context, req lfs.FilterRequest, ptr io.Reader, dst io.Writer) error {
		logger.Debug("smudge handler invoked", "pathname", req.Pathname)
		var downloadFn lfs.SmudgeDownloadFunc
		if drsCtx != nil {
			downloadFn = func(callCtx context.Context, oid, cachePath string) error {
				return downloader.DownloadToCachePath(callCtx, drsCtx, logger, oid, cachePath)
			}
		}
		return lfs.SmudgeContent(ctx, req.Pathname, ptr, dst, logger, downloadFn)
	}
}

// --------------------------------------------------------------------------
// Clean handler — stage: real file content → LFS pointer
// --------------------------------------------------------------------------

func makeCleanHandler(lfsRoot string, logger *slog.Logger) lfs.CleanFunc {
	return func(ctx context.Context, req lfs.FilterRequest, content io.Reader, dst io.Writer) error {
		logger.Debug("clean", "pathname", req.Pathname)
		return lfs.CleanContent(ctx, lfsRoot, req.Pathname, content, dst, logger)
	}
}

// --------------------------------------------------------------------------
// GitContext alias — expose the client package type without a circular import.
// --------------------------------------------------------------------------
