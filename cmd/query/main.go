package query

import (
	"encoding/json"
	"fmt"

	"github.com/bmeg/git-drs/client"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "query <drs_id>",
	Short: "Query DRS server by DRS ID",
	Long:  "Query DRS server by DRS ID",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		cfg, err := client.LoadConfig()
		if err != nil {
			return err
		}

		//fix this later
		baseURL := cfg.QueryServer.BaseURL

		client, err := client.NewIndexDClient(baseURL)
		if err != nil {
			return err
		}

		obj, err := client.QueryID(args[0])
		if err != nil {
			return err
		}
		out, err := json.MarshalIndent(*obj, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", string(out))
		return nil
	},
}
