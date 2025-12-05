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

		for k, v := range cfg.Remotes {
			tString := "NA"
			var remote config.DRSRemote
			if v.Gen3 != nil {
				tString = "gen3"
				remote = v.Gen3
			} else if v.Anvil != nil {
				tString = "anvil"
				remote = v.Anvil
			}
			fmt.Printf(" %s\t%s\t%s\n", k, tString, remote.GetEndpoint())

		}
		return nil
	},
}
