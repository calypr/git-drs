package remote

import (
	"fmt"
	"sort"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/spf13/cobra"
)

var RemoveCmd = &cobra.Command{
	Use:     "remove <remote-name>",
	Aliases: []string{"rm"},
	Short:   "Remove a DRS remote",
	Long:    "Remove a configured DRS remote and repair the default remote if needed.",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: requires exactly 1 argument (remote name), received %d\n\nUsage: %s\n\nRun 'git drs remote list' to see available remotes or 'git drs remote remove --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := config.Remote(args[0])
		logger := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if _, ok := cfg.Remotes[remoteName]; !ok {
			availableRemotes := make([]string, 0, len(cfg.Remotes))
			for name := range cfg.Remotes {
				availableRemotes = append(availableRemotes, string(name))
			}
			sort.Strings(availableRemotes)
			return fmt.Errorf(
				"remote '%s' not found.\nAvailable remotes: %v",
				remoteName,
				availableRemotes,
			)
		}

		updated, err := config.RemoveRemote(remoteName)
		if err != nil {
			return fmt.Errorf("failed to remove remote: %w", err)
		}

		if updated.DefaultRemote == "" {
			logger.Debug(fmt.Sprintf("Removed remote %s; no default remote remains", remoteName))
			return nil
		}

		logger.Debug(fmt.Sprintf("Removed remote %s; default remote is now %s", remoteName, updated.DefaultRemote))
		return nil
	},
}
