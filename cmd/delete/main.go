package delete

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var (
	dstPath string
)

// Cmd line declaration
// Cmd line declaration
var Cmd = &cobra.Command{
	Use:    "delete <remote> <hash-type> <oid>",
	Short:  "Delete a file using hash and file object ID",
	Long:   "Delete a file using file object ID. Use lfs ls-files to get oid",
	Hidden: true,
	Args:   cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		remote, hashType, oid := args[0], args[1], args[2]

		// check hash type is valid Checksum type and sha256
		if hashType != drs.ChecksumTypeSHA256.String() {
			return fmt.Errorf("Only sha256 supported, you requested to remove: %s", hashType)
		}

		logger := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		drsClient, err := cfg.GetRemoteClient(config.Remote(remote), logger)
		if err != nil {
			logger.Printf("error creating indexd client: %s", err)
			return err
		}

		// Delete the matching record
		err = drsClient.DeleteRecord(oid)
		if err != nil {
			return fmt.Errorf("Error deleting file for OID %s: %v", oid, err)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&dstPath, "dst", "d", "", "Destination path to save the downloaded file")
}
