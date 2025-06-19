package initialize

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/uc-cdis/gen3-client/gen3-client/jwt"
)

var (
	profile     string
	credFile    string
	apiEndpoint string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "init",
	Short: "initialize required setup for git-drs",
	Long:  "initialize hooks, config required for git-drs",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create .git/hooks/pre-commit file
		hooksDir := filepath.Join(".git", "hooks")
		preCommitPath := filepath.Join(hooksDir, "pre-commit")
		if err := os.MkdirAll(hooksDir, 0755); err != nil {
			fmt.Println("[ERROR] unable to create pre-commit hook file:", err)
			return err
		}
		hookContent := "#!/bin/sh\ngit drs precommit\n"
		if err := os.WriteFile(preCommitPath, []byte(hookContent), 0755); err != nil {
			fmt.Println("[ERROR] unable to write to pre-commit hook:", err)
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
				fmt.Printf("Error: unable to set git config %s: %v\n", cfg[0], err)
				return err
			}
		}

		// Call jwt.UpdateConfig with CLI parameters
		err := jwt.UpdateConfig(profile, apiEndpoint, credFile, "false", "")
		if err != nil {
			fmt.Printf("[ERROR] unable to configure your gen3 profile: %v\n", err)
			return err
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&profile, "profile", "", "Specify the profile to use")
	Cmd.MarkFlagRequired("profile")
	Cmd.Flags().StringVar(&credFile, "cred", "", "Specify the credential file that you want to use")
	Cmd.MarkFlagRequired("cred")
	Cmd.Flags().StringVar(&apiEndpoint, "apiendpoint", "", "Specify the API endpoint of the data commons")
	Cmd.MarkFlagRequired("apiendpoint")
}
