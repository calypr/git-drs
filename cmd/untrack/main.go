package untrack

import (
	"context"
	"fmt"

	"github.com/calypr/git-drs/internal/lfs"
	"github.com/spf13/cobra"
)

var gitLFSUntrackPatterns = lfs.GitLFSUntrackPatterns

var Cmd = NewCommand()

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "untrack <pattern> [pattern ...]",
		Short: "Stop tracking files with Git LFS",
		Long:  "Remove one or more Git LFS tracking patterns.",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runUntrack,
	}

	cmd.Flags().Bool("verbose", false, "show detailed output")
	cmd.Flags().Bool("dry-run", false, "show what would change without writing")

	return cmd
}

func runUntrack(cmd *cobra.Command, args []string) error {
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

	out, err := gitLFSUntrackPatterns(ctx, args, verbose, dryRun)
	if err != nil {
		return err
	}

	if out != "" {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
	}
	return nil
}
