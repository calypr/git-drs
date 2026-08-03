package smudge

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	internalfilter "github.com/calypr/git-drs/internal/filter"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/git-drs/internal/resolver"
	internaltransfer "github.com/calypr/git-drs/internal/transfer"
	"github.com/spf13/cobra"
)

// Cmd implements `git drs smudge`, which mirrors `git lfs smudge` behavior.
var Cmd = &cobra.Command{
	Use:   "smudge -- <path>",
	Short: "Smudge a file by converting an LFS pointer to file content (invoked by git)",
	Long: `git drs smudge reads potential LFS pointer content from stdin. If the input
is a valid LFS pointer, it restores file content from the local LFS object cache
or downloads it from the configured DRS remote. If no DRS remote is configured,
the pointer is passed through unchanged. If the input is not an LFS pointer,
content is passed through unchanged.

This command mirrors 'git lfs smudge' and is intended to be configured as the
git smudge filter:

  git config filter.drs.smudge 'git-drs smudge -- %f'`,
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runSmudge,
}

func runSmudge(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	pathname := args[0]
	logger := drslog.GetLogger()
	logger.Debug("smudge: starting", "pathname", pathname)

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("smudge: load config: %w", err)
	}

	remote, err := cfg.GetDefaultRemote()
	if err != nil {
		if errors.Is(err, config.ErrNoDefaultRemote) {
			logger.Debug("smudge: no default remote configured; passing through pointer", "pathname", pathname)
			return internalfilter.SmudgeContent(ctx, pathname, os.Stdin, os.Stdout, logger, nil)
		}
		return fmt.Errorf("smudge: get default remote: %w", err)
	}

	drsCtx, err := remoteruntime.New(cfg, remote, logger)
	if err != nil {
		return fmt.Errorf("smudge: create DRS client: %w", err)
	}

	var downloadFn internalfilter.SmudgeDownloadFunc
	if !internalfilter.ShouldSkipSmudge() {
		var terraResolver resolver.Resolver
		if drsCtx.RemoteType == config.TerraServerType {
			terraResolver, err = resolver.NewAnVIL(ctx, drsCtx.Endpoint)
			if err != nil {
				return fmt.Errorf("smudge: create Terra resolver: %w", err)
			}
		}
		downloadFn = func(callCtx context.Context, oid, cachePath string) error {
			if terraResolver != nil {
				if len(oid) >= 2 && oid[:2] == "//" {
					oid = "drs:" + oid
				}
				return resolver.DownloadToCache(callCtx, terraResolver, oid, cachePath)
			}
			return internaltransfer.DownloadToCachePath(callCtx, drsCtx, oid, cachePath)
		}
	}

	return internalfilter.SmudgeContent(ctx, pathname, os.Stdin, os.Stdout, logger, downloadFn)
}

func init() {}
