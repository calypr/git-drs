package list

import (
	"context"
	"fmt"

	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var remote string
var pretty = false

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List DRS objects in a DRS server",
	Long:  "List DRS objects in a DRS server",
	RunE: func(cmd *cobra.Command, args []string) error {

		logger := drslog.GetLogger()

		config, err := config.LoadConfig()
		if err != nil {
			return err
		}

		remoteName, err := config.GetRemoteOrDefault(remote)
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting remote: %v", err))
			return err
		}

		client, err := config.GetRemoteClient(remoteName, logger)
		if err != nil {
			return err
		}

		objs, err := client.API.SyfonClient().DRS().ListObjects(context.Background(), 1000, 1)
		if err != nil {
			return err
		}

		for _, drsObj := range objs.DrsObjects {
			if err := common.PrintDRSObject(drsObj, pretty); err != nil {
				return err
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().BoolVarP(&pretty, "pretty", "p", false, "pretty print JSON output")
}
