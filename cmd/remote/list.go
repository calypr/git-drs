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
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts no arguments, received %d\n\nUsage: %s\n\nSee 'git drs remote list --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
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
				remoteType = string(config.Gen3ServerType)
				remote = remoteSelect.Gen3
			} else if remoteSelect.Anvil != nil {
				remoteType = string(config.AnvilServerType)
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
