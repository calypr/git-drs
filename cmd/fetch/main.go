package fetch

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var runCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "fetch [remote-name]",
	Short: "Fetch LFS objects from remote via standard git-lfs",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs fetch --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		var remote config.Remote
		if len(args) > 0 {
			remote = config.Remote(args[0])
		} else {
			remote, err = cfg.GetDefaultRemote()
			if err != nil {
				logger.Error(fmt.Sprintf("Error getting remote: %v", err))
				return err
			}
		}

		drsClient, err := cfg.GetRemoteClient(remote, logger)
		if err != nil {
			logger.Error(fmt.Sprintf("\nerror creating DRS client: %s", err))
			return err
		}
		_ = drsClient // Remote validation only.

		out, err := runCommand("git", "lfs", "pull", string(remote))
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git lfs pull failed for remote %q: %s", remote, msg)
		}

		return nil
	},
}
