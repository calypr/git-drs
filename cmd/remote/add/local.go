package add

import (
	"fmt"
	"strings"

	"github.com/calypr/git-drs/client/local"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/spf13/cobra"
)

var LocalCmd = &cobra.Command{
	Use:   "local <remote-name> <url>",
	Short: "Add a local DRS server",
	Long:  "Add a local DRS server by specifying its base URL, e.g., http://localhost:8000. Optional --username/--password configures basic auth for git-lfs and helper flows.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]
		url := args[1]

		if url == "" {
			return fmt.Errorf("URL cannot be empty")
		}

		remoteSelect := config.RemoteSelect{
			Local: &local.LocalRemote{
				BaseURL:      url,
				ProjectID:    project,
				Bucket:       bucket,
				Organization: organization,
			},
		}

		newConfig, err := config.UpdateRemote(config.Remote(remoteName), remoteSelect)
		if err != nil {
			return err
		}
		if err := gitrepo.SetRemoteLFSURL(remoteName, url); err != nil {
			return fmt.Errorf("failed to configure lfs url for remote %q: %w", remoteName, err)
		}
		if err := gitrepo.ConfigureCredentialHelperForRepo(); err != nil {
			return fmt.Errorf("failed to configure git credential helper: %w", err)
		}
		if strings.TrimSpace(localUsername) != "" || strings.TrimSpace(localPassword) != "" {
			if strings.TrimSpace(localUsername) == "" || strings.TrimSpace(localPassword) == "" {
				return fmt.Errorf("both --username and --password are required when configuring local basic auth")
			}
			if err := gitrepo.SetRemoteBasicAuth(remoteName, strings.TrimSpace(localUsername), strings.TrimSpace(localPassword)); err != nil {
				return fmt.Errorf("failed to configure local basic auth for remote %q: %w", remoteName, err)
			}
		}

		fmt.Printf("Added remote '%s'. Config: %v\n", remoteName, newConfig.GetRemote(config.Remote(remoteName)))
		return nil
	},
}
