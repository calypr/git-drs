package add

import (
	"fmt"

	anvil_client "github.com/calypr/git-drs/client/anvil"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/log"
	"github.com/spf13/cobra"
)

var AnvilCmd = &cobra.Command{
	Use:  "anvil",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		_, err := config.UpdateRemote(config.ORIGIN, remoteAnvil)
		if err != nil {
			return fmt.Errorf("Error: unable to update config file: %v\n", err)
		}
	}

	// update current server in config
	cfg, err := config.UpdateCurrentRemote(config.ORIGIN)
	if err != nil {
		return fmt.Errorf("Error: unable to update current server to AnVIL: %v\n", err)
	}
	logger.Logf("Current server set to %s\n", cfg.GetCurrentRemote().GetEndpoint())

	return nil
}
