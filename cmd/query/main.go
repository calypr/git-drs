package query

import (
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
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		config, err := conf.LoadConfig()
		if err != nil {
			return err
		}

		var remoteName conf.Remote
		if remote != "" {
			remoteName = conf.Remote(remote)
		} else {
			remoteName, err = config.GetDefaultRemote()
			if err != nil {
				logger.Printf("Error getting default remote: %v", err)
				return err
			}
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
