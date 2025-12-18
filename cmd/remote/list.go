package remote

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List DRS repos",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			logg.Printf("Error loading config: %s", err)
			return err
		}

		for name, remoteSelect := range cfg.Remotes {
			// Determine if this is the default
			isDefault := name == cfg.DefaultRemote
			marker := " "
			if isDefault {
				marker = "*"
			}

			// Determine remote type and endpoint
			var remoteType string
			var remote config.DRSRemote
			if remoteSelect.Gen3 != nil {
				remoteType = "gen3"
				remote = remoteSelect.Gen3
			} else if remoteSelect.Anvil != nil {
				remoteType = "anvil"
				remote = remoteSelect.Anvil
			} else {
				remoteType = "unknown"
			}

			endpoint := "N/A"
			if remote != nil {
				endpoint = remote.GetEndpoint()
			}

			fmt.Printf("%s %-10s %-8s %s\n", marker, name, remoteType, endpoint)
		}
		return nil
	},
}
