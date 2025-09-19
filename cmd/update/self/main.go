package self

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var dir string

var Cmd = &cobra.Command{
	Use:   "self",
	Short: "Update git-drs",
	Long:  `Downloads, verifies, and installs the latest git-drs binary for your OS/architecture.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Self-update here...")
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
