package track

import (
	"context"
	"fmt"

	"github.com/calypr/git-drs/internal/lfs"
	"github.com/spf13/cobra"
)

var (
	gitLFSTrackPatterns = lfs.GitLFSTrackPatterns
	gitLFSListPatterns  = lfs.GitLFSListTrackedPatterns
)

var Cmd = NewCommand()

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "track [pattern ...]",
		Short: "Track files with Git LFS",
		Long:  "Manage Git LFS tracking patterns. With no patterns, this lists tracked patterns.",
		Args:  cobra.ArbitraryArgs,
		RunE:  runTrack,
	}

	cmd.Flags().Bool("verbose", false, "show detailed output")
	cmd.Flags().Bool("dry-run", false, "show what would change without writing")

	return cmd
}

func runTrack(cmd *cobra.Command, args []string) error {
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return fmt.Errorf("read flag verbose: %w", err)
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return fmt.Errorf("read flag dry-run: %w", err)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var out string
	if len(args) == 0 {
		out, err = gitLFSListPatterns(ctx, verbose)
	} else {
		out, err = gitLFSTrackPatterns(ctx, args, verbose, dryRun)
	}
	if err != nil {
		return err
	}

	if out != "" {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
	}
	return nil
}
