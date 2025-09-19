package drs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var dir string

var Cmd = &cobra.Command{
	Use:   "drs",
	Short: "Update DRS Downloader (dependency)",
	Long:  `Downloads, verifies, and installs the latest DRS Downloader binary`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("DRS Downloader update here...")
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
