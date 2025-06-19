package initialize

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "init",
	Short: "initialize required setup for git-drs",
	Long:  "initialize hooks and config required for git-drs",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create .git/hooks/pre-commit file
		hooksDir := filepath.Join(".git", "hooks")
		preCommitPath := filepath.Join(hooksDir, "pre-commit")
		if err := os.MkdirAll(hooksDir, 0755); err != nil {
			return err
		}
		hookContent := "#!/bin/sh\ngit drs precommit\n"
		if err := os.WriteFile(preCommitPath, []byte(hookContent), 0755); err != nil {
			return err
		}

		// set git config so git lfs uses gen3 custom transfer agent
		configs := [][]string{
			{"lfs.standalonetransferagent", "gen3"},
			{"lfs.customtransfer.gen3.path", "git-drs"},
			{"lfs.customtransfer.gen3.args", "transfer"},
			{"lfs.customtransfer.gen3.concurrent", "false"},
		}
		for _, cfg := range configs {
			cmd := exec.Command("git", "config", cfg[0], cfg[1])
			if err := cmd.Run(); err != nil {
				return err
			}
		}

		return nil
	},
}
