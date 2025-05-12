package download

import (
	"encoding/json"
	"fmt"

	"github.com/bmeg/git-drs/client"
	"github.com/spf13/cobra"
)

var server string = "https://calypr.ohsu.edu/ga4gh"

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "download",
	Short: "Query server for DRS ID",
	Long:  ``,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		client, err := client.NewIndexDClient(server)
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
