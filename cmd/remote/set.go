package remote

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var SetCmd = &cobra.Command{
	Use:   "set <remote-name>",
	Short: "Set the default DRS remote",
	Long:  "Set which DRS remote to use by default for all operations",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]
		logger := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// validate remote exists
		remote := config.Remote(remoteName)
		if _, ok := cfg.Remotes[remote]; !ok {
			availableRemotes := make([]string, 0, len(cfg.Remotes))
			for name := range cfg.Remotes {
				availableRemotes = append(availableRemotes, string(name))
			}
			return fmt.Errorf(
				"remote '%s' not found.\nAvailable remotes: %v",
				remoteName,
				availableRemotes,
			)
		}

		// save new default
		cfg.DefaultRemote = remote

		if err := config.SaveConfig(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		logger.Printf("Default remote set to: %s", remoteName)
		return nil
	},
}
