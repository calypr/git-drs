package rm

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/spf13/cobra"
)

var runCommand = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

var Cmd = &cobra.Command{
	Use:   "rm <path>...",
	Short: "Remove tracked git-drs files",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd.Context(), args)
	},
}

func run(ctx context.Context, args []string) error {
	tracked, err := lfs.GetTrackedLfsFiles(drslog.GetLogger())
	if err != nil {
		return err
	}

	planned := make([]string, 0, len(args))
	for _, raw := range args {
		path := filepath.ToSlash(filepath.Clean(raw))
		info, ok := tracked[path]
		if !ok || strings.TrimSpace(info.Oid) == "" {
			return fmt.Errorf("%s is not a tracked git-drs/LFS file", raw)
		}
		planned = append(planned, path)
	}

	gitArgs := []string{"rm", "--"}
	gitArgs = append(gitArgs, planned...)
	if err := runCommand("git", gitArgs...); err != nil {
		return err
	}

	return nil
}
