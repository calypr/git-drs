package add

import (
	"fmt"

	"github.com/calypr/git-drs/client/local"
	"github.com/calypr/git-drs/config"
	"github.com/spf13/cobra"
)

var LocalCmd = &cobra.Command{
	Use:   "local <remote-name> <url>",
	Short: "Add a local no-auth DRS server",
	Long:  "Add a local no-auth DRS server by specifying its base URL, e.g., http://localhost:8000",
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

		fmt.Printf("Added remote '%s'. Config: %v\n", remoteName, newConfig.GetRemote(config.Remote(remoteName)))
		return nil
	},
}
