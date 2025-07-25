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

		indexdClient, err := client.NewIndexDClient()
		if err != nil {
			fmt.Printf("\nerror creating indexd client: %s", err)
			return err
		}

		// get signed url
		accessUrl, err := indexdClient.GetDownloadURL(oid)
		if err != nil {
			return fmt.Errorf("Error downloading file for OID %s: %v", oid, err)
		}
		if accessUrl.URL == "" {
			return fmt.Errorf("Unable to get access URL %s", oid)
		}

		// download url to destination path or LFS objects if not specified
		if dstPath == "" {
			dstPath, err = client.GetObjectPath(client.LFS_OBJS_PATH, oid)
		}
		if err != nil {
			return fmt.Errorf("Error getting destination path for OID %s: %v", oid, err)
		}
		err = client.DownloadSignedUrl(accessUrl.URL, dstPath)
		if err != nil {
			return fmt.Errorf("Error downloading file for OID %s: %v", oid, err)
		}

		if err != nil {
			return fmt.Errorf("\nerror downloading file object ID %s: %s", oid, err)
		}

		fmt.Println("file downloaded")

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&dstPath, "dst", "d", "", "Destination path to save the downloaded file")
}
