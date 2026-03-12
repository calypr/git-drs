package add

import (
	"fmt"
	"log/slog"

	anvil_client "github.com/calypr/git-drs/client/anvil"
	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var AnvilCmd = &cobra.Command{
	Use: "anvil [remote-name]",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs remote add anvil --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("error: anvil remote is not yet implemented. Use 'git drs remote add gen3' instead. See 'git drs remote add gen3 --help' for more details")
	},
}

func anvilInit(terraProject string, logger *slog.Logger) error {
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

		// Ensure Git DRS is fully initialized (hooks, LFS config, etc.)
		logg := drslog.GetLogger()
		if newlyInitialized, err := initialize.InitializeRepo(logg, 1, false, 500, false); err != nil {
			logg.Warn(fmt.Sprintf("Warning: failed to automatically initialize Git DRS: %v", err))
		} else if newlyInitialized {
			logg.Debug("Automatically initialized Git DRS (hooks and LFS config)")
		}
	}

	return nil
}
