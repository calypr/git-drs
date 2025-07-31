package delete

import (
	"fmt"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drs"
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
	Use:   "delete <oid>",
	Short: "Delete a file using file object ID",
	Long:  "Delete a file using file object ID (sha256 hash). Use lfs ls-files to get oid",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		oid := args[0]

		logger, err := client.NewLogger("")
		if err != nil {
			return err
		}
		defer logger.Close()

		indexdClient, err := client.NewIndexDClient(logger)
		if err != nil {
			logger.Logf("error creating indexd client: %s", err)
			return err
		}
		// get signed url
		oidObject, err := indexdClient.GetObjectByHash("sha256", oid)
		if err != nil {
			return fmt.Errorf("Error downloading file for OID %s: %v", oid, err)
		}

		err = indexdClient.DeleteIndexdRecord(oidObject.Id)
		if err != nil {
			return fmt.Errorf("Error deleting file for OID %s: %v", oid, err)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&dstPath, "dst", "d", "", "Destination path to save the downloaded file")
}
