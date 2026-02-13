package remote

import (
	"fmt"
	"sort"

	"github.com/calypr/git-drs/config"
	"github.com/spf13/cobra"
)

var RemoveCmd = &cobra.Command{
	Use:     "remove <remote-name>",
	Aliases: []string{"rm"},
	Short:   "Remove a configured DRS remote",
	Long:    "Remove a configured DRS remote and update the default remote if needed",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: requires exactly 1 argument (remote name), received %d\n\nUsage: %s\n\nRun 'git drs remote list' to see available remotes or 'git drs remote rm --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		remote := config.Remote(remoteName)
		if _, ok := cfg.Remotes[remote]; !ok {
			availableRemotes := make([]string, 0, len(cfg.Remotes))
			for name := range cfg.Remotes {
				availableRemotes = append(availableRemotes, string(name))
			}
			sort.Strings(availableRemotes)
			return fmt.Errorf("remote '%s' not found.\nAvailable remotes: %v", remoteName, availableRemotes)
		}

		if err := config.RemoveRemote(remote); err != nil {
			return fmt.Errorf("failed to remove remote: %w", err)
		}

		return nil
	},
}
