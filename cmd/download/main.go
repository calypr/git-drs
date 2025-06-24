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
	Use:   "download <oid>",
	Short: "Download file using file object ID",
	Long:  "Download file using file object ID (sha256 hash). Use lfs ls-files to get oid",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		oid := args[0]

		client, err := client.NewIndexDClient()
		if err != nil {
			fmt.Printf("\nerror creating indexd client: %s", err)
			return err
		}

		fmt.Println("created indexd client")

		_, err = client.GetDownloadURL(oid)
		if err != nil {
			fmt.Printf("\nerror downloading file object ID %s: %s", oid, err)
			return err
		}

		fmt.Println("file downloaded")

		return nil
	},
}
