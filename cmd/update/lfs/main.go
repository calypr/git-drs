package lfs

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var dir string

var Cmd = &cobra.Command{
	Use:   "lfs",
	Short: "Update git-lfs (dependency)",
	Long:  `Downloads, verifies, and installs the latest git-lfs binary for your OS/architecture.`,
	Run: func(cmd *cobra.Command, args []string) {

		if err := os.MkdirAll(dir, 0755); err != nil {
			os.Exit(1)
		}

		if err := InstallLatest(dir); err != nil {
			os.Exit(1)
		}

		if err := RunGitLFSInstall(); err != nil {
			os.Exit(1)
		}
	},
}

func init() {
	home, err := os.UserHomeDir()
	defaultDir := "~/.local/bin"
	if err == nil {
		defaultDir = filepath.Join(home, ".local", "bin")
	}
	Cmd.Flags().StringVarP(&dir, "dir", "d", defaultDir, "Installation directory for git-drs binary")
}
