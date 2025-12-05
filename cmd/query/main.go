package query

import (
	"encoding/json"

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
		client, err := config.GetRemoteClient(conf.Remote(args[0]), logger)
		if err != nil {
			return err
		}

		obj, err := client.GetObject(args[1])
		if err != nil {
			return err
		}
		out, err := json.MarshalIndent(*obj, "", "  ")
		if err != nil {
			return err
		}
		logger.Printf("%s\n", string(out))
		return nil
	},
}
