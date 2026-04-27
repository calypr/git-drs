package clean

// Package clean implements the `git drs clean` sub command. It reads raw file
// content from stdin, stores the content in the local object cache, and writes
// an LFS pointer to stdout. A DRS map entry is also recorded so that
// `git drs push` can upload the object to the configured DRS server.
//
// git configures this command as the clean filter when
//
//	filter.lfs.clean = git-drs clean -- %f
//
// is set in the repository config (written by `git drs init`).

import (
	"context"
	"fmt"
	"os"

	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/spf13/cobra"
)

// Cmd is the cobra command registered in cmd/root.go.
var Cmd = &cobra.Command{
	Use:   "clean -- <path>",
	Short: "Clean a file by converting its content to an LFS pointer (invoked by git)",
	Long: `git drs clean reads raw file content from stdin, hashes it, stores it in
the local object cache (.git/lfs/objects), and writes an LFS pointer
to stdout.  It also records a DRS map entry so that 'git drs push' can upload
the object to the configured DRS server.

This command is intended to be configured as the git clean filter:

  git config filter.lfs.clean 'git-drs clean -- %f'`,
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runClean,
}

func runClean(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	pathname := args[0]
	logger := drslog.GetLogger()
	logger.Debug("clean: starting", "pathname", pathname)

	_, lfsRoot, err := lfs.GetGitRootDirectories(ctx)
	if err != nil {
		return fmt.Errorf("clean: resolve LFS root: %w", err)
	}

	return lfs.CleanContent(ctx, lfsRoot, pathname, os.Stdin, os.Stdout, logger)
}
