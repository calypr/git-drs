package download

import (
	"fmt"

	"github.com/bmeg/git-drs/client"
	"github.com/bmeg/git-drs/drs"
	"github.com/spf13/cobra"
)

var (
	server  string
	dstPath string
	drsObj  *drs.DRSObject
)

// Cmd line declaration
// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "download <drsId> <accessId>",
	Short: "Download file using DRS ID and access ID",
	Long:  "Download file using DRS ID and access ID. The access ID is the access method used to download the file.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		drsId := args[0]
		accessId := args[1]
		cfg, err := client.LoadConfig()
		if err != nil {
			return err
		}

		baseURL := cfg.QueryServer.BaseURL

		// print random string to stdout
		fmt.Println("Using server:", cfg.QueryServer.BaseURL)

		client, err := client.NewIndexDClient(baseURL)
		if err != nil {
			fmt.Printf("\nerror creating indexd client: %s", err)
			return err
		}

		fmt.Println("created indexd client:", cfg.QueryServer.BaseURL)

		if dstPath == "" {

			drsObj, err = client.QueryID(drsId)
			if err != nil {
				fmt.Printf("\nerror querying DRS ID %s: %s", drsId, err)
				return err
			}
			dstPath = drsObj.Name
		}

		fmt.Println("downloading file:", drsObj.Name)

		_, err = client.DownloadFile(drsId, accessId, dstPath)
		if err != nil {
			fmt.Printf("\nerror downloading file %s: %s", drsId, err)
			return err
		}

		fmt.Println("file downloaded")

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&dstPath, "dstPath", "d", "", "Optional destination file path")
}
