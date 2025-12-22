package query

import (
	"fmt"

	"github.com/bytedance/sonic"
	conf "github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var remote string

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "query <drs_id>",
	Short: "Query DRS server by DRS ID",
	Long:  "Query DRS server by DRS ID",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: requires exactly 1 argument (DRS ID), received %d\n\nUsage: %s\n\nSee 'git drs query --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		config, err := conf.LoadConfig()
		if err != nil {
			return err
		}

		remoteName, err := config.GetRemoteOrDefault(remote)
		if err != nil {
			logger.Printf("Error getting remote: %v", err)
			return err
		}

		client, err := config.GetRemoteClient(remoteName, logger)
		if err != nil {
			return err
		}

		obj, err := client.GetObject(args[0])
		if err != nil {
			return err
		}
		out, err := sonic.ConfigFastest.MarshalIndent(*obj, "", "  ")
		if err != nil {
			return err
		}
		logger.Printf("%s\n", string(out))
		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
}
