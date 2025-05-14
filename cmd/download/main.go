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
	Short: "Download file using s3",
	Long:  ``,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		client, err := client.NewIndexDClient(server)
		if err != nil {
			return err
		}

		// // get file name from DRS object
		// drs_obj, err := client.QueryID(args[0])
		// if err != nil {
		// 	return err
		// }

		access_url, err := client.DownloadFile(args[0], "s3", "cbds-prod", "./file.txt")
		if err != nil {
			return err
		}

		out, err := json.MarshalIndent(*access_url, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", string(out))
		return nil
	},
}
