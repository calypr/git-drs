package add

import (
	"fmt"

	"log"

	anvil_client "github.com/calypr/git-drs/client/anvil"
	"github.com/calypr/git-drs/config"
	"github.com/spf13/cobra"
)

var AnvilCmd = &cobra.Command{
	Use:  "anvil",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: actuallly implement
		fmt.Printf("NOT IMPLEMENTED")
		return nil
	},
}

func anvilInit(terraProject string, logger *log.Logger) error {
	// make sure terra project is provided
	if terraProject != "" {
		// populate anvil config
		remoteAnvil := config.RemoteSelect{
			Anvil: &anvil_client.AnvilRemote{
				Endpoint: anvil_client.ANVIL_ENDPOINT,
				Auth: anvil_client.AnvilAuth{
					TerraProject: terraProject,
				},
			},
		}
		// TODO: different than ORIGIN?
		remoteName := config.Remote(config.AnvilServerType)
		_, err := config.UpdateRemote(remoteName, remoteAnvil)
		if err != nil {
			return fmt.Errorf("Error: unable to update config file: %v\n", err)
		}

	}

	return nil
}
